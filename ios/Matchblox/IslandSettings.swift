import ActivityKit
import Foundation
import MatchbloxKit
import os

let islandLog = Logger(subsystem: "matchblox", category: "island")

/// The Island setting and what the app last registered, in the App Group, where the
/// widget and the buzz read them.
enum IslandSettings {
    /// `group.<MATCHBLOX_BUNDLE_ID>`, from the Info key `MatchbloxAppGroup`.
    static let appGroup = Bundle.main.object(forInfoDictionaryKey: "MatchbloxAppGroup") as? String ?? ""

    private static var defaults: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }
    private static let prefsKey = IslandShelf.prefsKey
    private static func recordKey(_ host8: String) -> String { "island-registered-\(host8)" }
    private static func channelKey(_ host8: String) -> String { "island-channel-\(host8)" }

    /// Follow, Show and Buzz. Defaults: Sessions, the host opened last, Buzz on.
    static var prefs: IslandPrefs {
        get { read(prefsKey) ?? IslandPrefs() }
        set { write(newValue, prefsKey) }
    }

    /// The last APNs device token iOS gave, kept across launches.
    static var keptDeviceToken: String? {
        get { defaults.string(forKey: "island-device-token") }
        set { defaults.set(newValue, forKey: "island-device-token") }
    }

    /// The last push-to-start token iOS gave, kept across launches.
    static var keptStartToken: String? {
        get { defaults.string(forKey: "island-start-token") }
        set { defaults.set(newValue, forKey: "island-start-token") }
    }

    static func record(host: Host) -> RegistrationRecord? { read(recordKey(host8(host.fingerprint))) }
    static func setRecord(_ record: RegistrationRecord, host: Host) { write(record, recordKey(host8(host.fingerprint))) }

    static func channel(host: Host) -> ChannelGrant? { read(channelKey(host8(host.fingerprint))) }
    static func setChannel(_ channel: ChannelGrant, host: Host) { write(channel, channelKey(host8(host.fingerprint))) }

    /// The names of the numbers, as `island-names-<host8>.json` in the App Group, and the
    /// host's short name, which the island shows after `▰`.
    static func saveNames(_ names: IslandNames, host: Host) {
        defaults.set(host.label, forKey: IslandShelf.labelKey(host: host8(host.fingerprint)))
        guard let dir = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup) else {
            islandLog.error("no App Group container: the names are not kept")
            return
        }
        let file = dir.appendingPathComponent(IslandNames.fileName(host: host8(host.fingerprint)))
        do {
            try JSONEncoder().encode(names).write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        } catch {
            islandLog.error("the names are not kept: \(String(describing: error), privacy: .public)")
        }
    }

    /// Ends each activity whose host is not the followed host: an old host that still pushes
    /// never keeps a second island.
    @MainActor
    static func endStrayActivities() {
        let prefs = IslandSettings.prefs
        Task { @MainActor in
            for activity in Activity<IslandAttributes>.activities where prefs.isStray(activityHost: activity.attributes.host) {
                await activity.end(nil, dismissalPolicy: .immediate)
            }
        }
    }

    /// A Live Activity does not draw again when the setting changes: after a change of Show
    /// or Follow, the activity of the followed host is updated with its own content, so the
    /// widget picks the face again, and an activity of a host no longer followed ends.
    @MainActor
    static func refreshActivities() {
        let prefs = IslandSettings.prefs
        Task { @MainActor in
            for activity in Activity<IslandAttributes>.activities {
                if prefs.isStray(activityHost: activity.attributes.host) {
                    await activity.end(nil, dismissalPolicy: .immediate)
                    continue
                }
                let state = activity.content.state
                await activity.update(ActivityContent(state: state, staleDate: IslandLook.staleDate(state, now: .now)))
            }
        }
    }

    private static func read<T: Decodable>(_ key: String) -> T? {
        guard let data = defaults.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    private static func write<T: Encodable>(_ value: T, _ key: String) {
        guard let data = try? JSONEncoder().encode(value) else { return }
        defaults.set(data, forKey: key)
    }
}
