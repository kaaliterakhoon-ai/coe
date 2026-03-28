#include <dbus/dbus.h>

#include <atomic>
#include <cctype>
#include <chrono>
#include <cstdlib>
#include <fstream>
#include <memory>
#include <sstream>
#include <string>
#include <thread>
#include <unistd.h>
#include <vector>

#include <fcitx/addonfactory.h>
#include <fcitx/addoninstance.h>
#include <fcitx/addonmanager.h>
#include <fcitx/event.h>
#include <fcitx/inputcontext.h>
#include <fcitx/inputpanel.h>
#include <fcitx-utils/eventdispatcher.h>
#include <fcitx/instance.h>
#include <fcitx-utils/key.h>
#include <fcitx-utils/log.h>

namespace {

constexpr const char *kServiceName = "com.mistermorph.Coe";
constexpr const char *kObjectPath = "/com/mistermorph/Coe";
constexpr const char *kInterfaceName = "com.mistermorph.Coe.Dictation1";
constexpr const char *kDefaultTriggerKey = "Shift+Super+D";

std::string debugMarkerPath() {
    std::ostringstream out;
    out << "/tmp/coe-fcitx-" << getuid() << ".log";
    return out.str();
}

void appendDebugMarker(const std::string &message) {
    std::ofstream out(debugMarkerPath(), std::ios::app);
    if (!out.is_open()) {
        return;
    }
    const auto now = std::chrono::system_clock::now();
    const auto millis =
        std::chrono::duration_cast<std::chrono::milliseconds>(
            now.time_since_epoch())
            .count();
    out << millis << " " << message << "\n";
}

std::string trim(std::string value) {
    while (!value.empty() &&
           std::isspace(static_cast<unsigned char>(value.front()))) {
        value.erase(value.begin());
    }
    while (!value.empty() &&
           std::isspace(static_cast<unsigned char>(value.back()))) {
        value.pop_back();
    }
    return value;
}

struct TriggerKeyConfig {
    fcitx::Key key;
    std::string source;
    std::string warning;
};

std::string normalizeModifierToken(const std::string &token) {
    auto lowered = token;
    for (auto &ch : lowered) {
        ch = static_cast<char>(std::tolower(static_cast<unsigned char>(ch)));
    }
    if (lowered == "ctrl" || lowered == "control") {
        return "Control";
    }
    if (lowered == "alt") {
        return "Alt";
    }
    if (lowered == "shift") {
        return "Shift";
    }
    if (lowered == "super" || lowered == "meta" || lowered == "win" ||
        lowered == "windows") {
        return "Super";
    }
    return token;
}

std::string normalizeKeyToken(const std::string &token) {
    if (token.empty()) {
        return "";
    }
    if (token.size() == 1 &&
        std::isalpha(static_cast<unsigned char>(token.front()))) {
        return std::string(
            1, static_cast<char>(std::toupper(static_cast<unsigned char>(token.front()))));
    }

    auto lowered = token;
    for (auto &ch : lowered) {
        ch = static_cast<char>(std::tolower(static_cast<unsigned char>(ch)));
    }
    if (lowered == "ctrl") {
        return "Control";
    }
    if (lowered == "enter") {
        return "Return";
    }
    if (lowered == "esc") {
        return "Escape";
    }
    if (lowered.size() >= 2 && lowered.front() == 'f' &&
        std::isdigit(static_cast<unsigned char>(lowered[1]))) {
        auto normalized = lowered;
        normalized[0] = 'F';
        return normalized;
    }
    return token;
}

std::string acceleratorToFcitxKeyString(const std::string &value) {
    auto accelerator = trim(value);
    if (accelerator.empty()) {
        return "";
    }
    if (accelerator.front() != '<') {
        return accelerator;
    }

    std::vector<std::string> parts;
    while (!accelerator.empty() && accelerator.front() == '<') {
        const auto end = accelerator.find('>');
        if (end == std::string::npos) {
            return "";
        }
        auto token = trim(accelerator.substr(1, end - 1));
        if (token.empty()) {
            return "";
        }
        parts.push_back(normalizeModifierToken(token));
        accelerator = trim(accelerator.substr(end + 1));
    }

    auto key = normalizeKeyToken(trim(accelerator));
    if (key.empty()) {
        return "";
    }
    parts.push_back(key);

    std::ostringstream out;
    for (size_t i = 0; i < parts.size(); ++i) {
        if (i > 0) {
            out << "+";
        }
        out << parts[i];
    }
    return out.str();
}

std::string panelTextForState(const std::string &state) {
    if (state == "recording") {
        return "Coe: Listening...";
    }
    if (state == "processing") {
        return "Coe: Processing...";
    }
    return "";
}

class CoeModule final : public fcitx::AddonInstance {
public:
    explicit CoeModule(fcitx::Instance *instance)
        : instance_(instance),
          triggerKey_(fcitx::Key(kDefaultTriggerKey).normalize()),
          triggerKeySource_("default") {
        if (!instance_) {
            FCITX_ERROR() << "coe-fcitx: missing fcitx instance";
            appendDebugMarker("init error missing-instance");
            return;
        }

        if (!triggerKey_.isValid()) {
            FCITX_ERROR() << "coe-fcitx: invalid trigger key";
            appendDebugMarker("init error invalid-trigger-key");
            return;
        }

        dbus_threads_init_default();
        dispatcher_.attach(&instance_->eventLoop());
        connectCallBus();
        refreshTriggerKeyFromDaemon();
        connectSignalBus();
        keyWatcher_ = instance_->watchEvent(
            fcitx::EventType::InputContextKeyEvent,
            fcitx::EventWatcherPhase::PreInputMethod,
            [this](fcitx::Event &event) { this->handleKeyEvent(event); });
        startSignalLoop();
        FCITX_INFO() << "coe-fcitx: module initialized with trigger "
                     << triggerKey_.toString() << " source="
                     << triggerKeySource_;
        appendDebugMarker("init ok trigger=" + triggerKey_.toString() +
                          " source=" + triggerKeySource_);
    }

    ~CoeModule() override {
        running_ = false;
        if (signalThread_.joinable()) {
            signalThread_.join();
        }
        closeBus(signalBus_);
        closeBus(callBus_);
        dispatcher_.detach();
    }

private:
    static void closeBus(DBusConnection *&bus) {
        if (!bus) {
            return;
        }
        dbus_connection_close(bus);
        dbus_connection_unref(bus);
        bus = nullptr;
    }

    DBusConnection *connectPrivateBus(const char *purpose) {
        DBusError err;
        dbus_error_init(&err);
        DBusConnection *bus = dbus_bus_get_private(DBUS_BUS_SESSION, &err);
        if (dbus_error_is_set(&err)) {
            FCITX_ERROR() << "coe-fcitx: failed to connect " << purpose
                          << " bus: "
                          << err.message;
            dbus_error_free(&err);
            return nullptr;
        }
        if (!bus) {
            FCITX_ERROR() << "coe-fcitx: session bus is unavailable for "
                          << purpose;
            return nullptr;
        }
        dbus_connection_set_exit_on_disconnect(bus, false);
        return bus;
    }

    void connectCallBus() { callBus_ = connectPrivateBus("call"); }

    void refreshTriggerKeyFromDaemon() {
        if (!callBus_) {
            return;
        }

        DBusMessage *message = dbus_message_new_method_call(
            kServiceName, kObjectPath, kInterfaceName, "TriggerKey");
        if (!message) {
            FCITX_WARN() << "coe-fcitx: failed to allocate TriggerKey() D-Bus message";
            return;
        }

        DBusError err;
        dbus_error_init(&err);
        DBusMessage *reply = dbus_connection_send_with_reply_and_block(
            callBus_, message, 1500, &err);
        dbus_message_unref(message);

        if (dbus_error_is_set(&err)) {
            FCITX_WARN() << "coe-fcitx: TriggerKey() failed, using default: "
                         << err.name << " " << err.message;
            appendDebugMarker("trigger-key fallback default dbus-error");
            dbus_error_free(&err);
            return;
        }
        if (!reply) {
            FCITX_WARN() << "coe-fcitx: TriggerKey() returned no reply, using default";
            appendDebugMarker("trigger-key fallback default no-reply");
            return;
        }

        const char *triggerKey = "";
        if (!dbus_message_get_args(reply, &err, DBUS_TYPE_STRING, &triggerKey,
                                   DBUS_TYPE_INVALID)) {
            FCITX_WARN() << "coe-fcitx: failed to parse TriggerKey() reply, using default: "
                         << err.message;
            appendDebugMarker("trigger-key fallback default parse-error");
            dbus_error_free(&err);
            dbus_message_unref(reply);
            return;
        }

        const auto rawValue = std::string(triggerKey ? triggerKey : "");
        const auto fcitxKeyString = acceleratorToFcitxKeyString(rawValue);
        auto parsedKey = fcitx::Key(fcitxKeyString).normalize();
        if (!parsedKey.isValid()) {
            FCITX_WARN() << "coe-fcitx: invalid trigger key from daemon '"
                         << rawValue << "', using default";
            appendDebugMarker("trigger-key fallback default invalid-daemon-value");
            dbus_message_unref(reply);
            return;
        }

        triggerKey_ = parsedKey;
        triggerKeySource_ = "daemon";
        appendDebugMarker("trigger-key loaded raw=" + rawValue + " fcitx=" +
                          triggerKey_.toString());
        dbus_message_unref(reply);
    }

    void connectSignalBus() {
        signalBus_ = connectPrivateBus("signal");
        if (!signalBus_) {
            return;
        }

        addSignalMatch(
            "type='signal',sender='com.mistermorph.Coe',interface='com.mistermorph.Coe.Dictation1',member='StateChanged',path='/com/mistermorph/Coe'");
        addSignalMatch(
            "type='signal',sender='com.mistermorph.Coe',interface='com.mistermorph.Coe.Dictation1',member='ResultReady',path='/com/mistermorph/Coe'");
        addSignalMatch(
            "type='signal',sender='com.mistermorph.Coe',interface='com.mistermorph.Coe.Dictation1',member='ErrorRaised',path='/com/mistermorph/Coe'");
    }

    void addSignalMatch(const char *rule) {
        if (!signalBus_) {
            return;
        }
        DBusError err;
        dbus_error_init(&err);
        dbus_bus_add_match(signalBus_, rule, &err);
        dbus_connection_flush(signalBus_);
        if (dbus_error_is_set(&err)) {
            FCITX_WARN() << "coe-fcitx: failed to add D-Bus match: "
                         << err.message;
            dbus_error_free(&err);
        }
    }

    void startSignalLoop() {
        if (!signalBus_) {
            return;
        }
        running_ = true;
        signalThread_ = std::thread([this]() { this->signalLoop(); });
    }

    void signalLoop() {
        while (running_) {
            dbus_connection_read_write(signalBus_, 200);
            while (DBusMessage *message = dbus_connection_pop_message(signalBus_)) {
                handleSignal(message);
                dbus_message_unref(message);
            }
        }
    }

    void handleSignal(DBusMessage *message) {
        if (!message) {
            return;
        }

        if (dbus_message_is_signal(message, kInterfaceName, "StateChanged")) {
            const char *state = "";
            const char *sessionID = "";
            const char *detail = "";
            DBusError err;
            dbus_error_init(&err);
            if (!dbus_message_get_args(message, &err, DBUS_TYPE_STRING, &state,
                                       DBUS_TYPE_STRING, &sessionID,
                                       DBUS_TYPE_STRING, &detail,
                                       DBUS_TYPE_INVALID)) {
                FCITX_WARN() << "coe-fcitx: failed to parse StateChanged: "
                             << err.message;
                dbus_error_free(&err);
                return;
            }
            auto stateText = std::string(state ? state : "");
            auto detailText = std::string(detail ? detail : "");
            FCITX_DEBUG() << "coe-fcitx: state changed to " << stateText;
            appendDebugMarker("state " + stateText);
            dispatcher_.schedule([this, stateText, detailText]() {
                this->updatePanelState(stateText, detailText);
            });
            return;
        }

        if (dbus_message_is_signal(message, kInterfaceName, "ResultReady")) {
            const char *sessionID = "";
            const char *text = "";
            DBusError err;
            dbus_error_init(&err);
            if (!dbus_message_get_args(message, &err, DBUS_TYPE_STRING,
                                       &sessionID, DBUS_TYPE_STRING, &text,
                                       DBUS_TYPE_INVALID)) {
                FCITX_WARN() << "coe-fcitx: failed to parse ResultReady: "
                             << err.message;
                dbus_error_free(&err);
                return;
            }
            FCITX_DEBUG() << "coe-fcitx: received ResultReady for session "
                          << (sessionID ? sessionID : "") << " with "
                          << (text ? std::string(text).size() : 0)
                          << " bytes";
            appendDebugMarker(
                std::string("result session=") + (sessionID ? sessionID : "") +
                " bytes=" +
                std::to_string(text ? std::string(text).size() : 0));
            auto committedText = std::string(text ? text : "");
            dispatcher_.schedule([this, committedText]() {
                this->commitResult(committedText);
            });
            return;
        }

        if (dbus_message_is_signal(message, kInterfaceName, "ErrorRaised")) {
            const char *sessionID = "";
            const char *errorText = "";
            DBusError err;
            dbus_error_init(&err);
            if (!dbus_message_get_args(message, &err, DBUS_TYPE_STRING,
                                       &sessionID, DBUS_TYPE_STRING,
                                       &errorText, DBUS_TYPE_INVALID)) {
                FCITX_WARN() << "coe-fcitx: failed to parse ErrorRaised: "
                             << err.message;
                dbus_error_free(&err);
                return;
            }
            FCITX_WARN() << "coe-fcitx: daemon error for session "
                         << (sessionID ? sessionID : "") << ": "
                         << (errorText ? errorText : "");
        }
    }

    void updatePanelState(const std::string &state, const std::string &) {
        auto *inputContext = instance_->lastFocusedInputContext();
        if (!inputContext || !inputContext->hasFocus()) {
            appendDebugMarker("panel skipped no-focused-input-context state=" +
                              state);
            return;
        }

        fcitx::Text auxText;
        const auto text = panelTextForState(state);
        if (!text.empty()) {
            auxText.append(text);
        }
        inputContext->inputPanel().setAuxUp(auxText);
        inputContext->updateUserInterface(
            fcitx::UserInterfaceComponent::InputPanel, true);

        if (text.empty()) {
            FCITX_DEBUG() << "coe-fcitx: cleared panel state";
            appendDebugMarker("panel clear state=" + state);
            return;
        }

        FCITX_DEBUG() << "coe-fcitx: panel state " << text;
        appendDebugMarker("panel show state=" + state + " text=" + text);
    }

    void commitResult(const std::string &text) {
        if (text.empty()) {
            FCITX_WARN() << "coe-fcitx: empty result text";
            appendDebugMarker("commit skipped empty-text");
            return;
        }

        FCITX_DEBUG() << "coe-fcitx: attempting to commit " << text.size()
                      << " bytes to current input context";
        auto *inputContext = instance_->lastFocusedInputContext();
        if (!inputContext || !inputContext->hasFocus()) {
            FCITX_WARN() << "coe-fcitx: no focused input context for result";
            appendDebugMarker("commit skipped no-focused-input-context");
            return;
        }

        inputContext->commitString(text);
        FCITX_INFO() << "coe-fcitx: committed " << text.size()
                     << " bytes to current input context";
        appendDebugMarker("commit ok bytes=" + std::to_string(text.size()));
    }

    void handleKeyEvent(fcitx::Event &event) {
        auto &keyEvent = static_cast<fcitx::KeyEvent &>(event);
        if (keyEvent.isRelease()) {
            return;
        }
        if (!keyEvent.inputContext()) {
            return;
        }
        if (!keyEvent.inputContext()->hasFocus()) {
            return;
        }
        if (!keyEvent.key().check(triggerKey_)) {
            return;
        }

        FCITX_DEBUG() << "coe-fcitx: trigger matched for " << triggerKey_.toString();
        appendDebugMarker("trigger matched key=" + triggerKey_.toString());
        if (!callToggle()) {
            FCITX_WARN() << "coe-fcitx: failed to call Coe Toggle() over D-Bus";
            appendDebugMarker("toggle failed");
            return;
        }

        keyEvent.filterAndAccept();
    }

    bool callToggle() {
        if (!callBus_) {
            connectCallBus();
            if (!callBus_) {
                return false;
            }
        }

        DBusMessage *message = dbus_message_new_method_call(
            kServiceName, kObjectPath, kInterfaceName, "Toggle");
        if (!message) {
            FCITX_ERROR() << "coe-fcitx: failed to allocate D-Bus message";
            return false;
        }

        DBusError err;
        dbus_error_init(&err);
        DBusMessage *reply = dbus_connection_send_with_reply_and_block(
            callBus_, message, 2000, &err);
        dbus_message_unref(message);

        if (dbus_error_is_set(&err)) {
            FCITX_WARN() << "coe-fcitx: Toggle() failed: " << err.name << " "
                         << err.message;
            dbus_error_free(&err);
            return false;
        }
        if (!reply) {
            FCITX_WARN() << "coe-fcitx: Toggle() returned no reply";
            return false;
        }

        dbus_message_unref(reply);
        FCITX_DEBUG() << "coe-fcitx: Toggle() completed successfully";
        appendDebugMarker("toggle ok");
        return true;
    }

    fcitx::Instance *instance_;
    fcitx::Key triggerKey_;
    std::string triggerKeySource_;
    fcitx::EventDispatcher dispatcher_;
    DBusConnection *callBus_ = nullptr;
    DBusConnection *signalBus_ = nullptr;
    std::unique_ptr<fcitx::HandlerTableEntry<fcitx::EventHandler>> keyWatcher_;
    std::thread signalThread_;
    std::atomic<bool> running_ = false;
};

class CoeModuleFactory final : public fcitx::AddonFactory {
public:
    fcitx::AddonInstance *create(fcitx::AddonManager *manager) override {
        return new CoeModule(manager ? manager->instance() : nullptr);
    }
};

} // namespace

FCITX_ADDON_FACTORY(CoeModuleFactory)
