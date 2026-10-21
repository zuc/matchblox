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
    private static let prefsKey = "island-prefs"
    private static func recordKey(_ host8: String) -> String { "island-registered-\(host8)" }
    private static func channelKey(_ host8: String) -> String { "island-channel-\(host8)" }

    /// Follow, Show and Buzz. Defaults: Sessions, the host opened last, Buzz on.
    static var prefs: IslandPrefs {
        get { read(prefsKey) ?? IslandPrefs() }
        set { write(newValue, prefsKey) }
    }

    static func record(host: Host) -> RegistrationRecord? { read(recordKey(host8(host.fingerprint))) }
    static func setRecord(_ record: RegistrationRecord, host: Host) { write(record, recordKey(host8(host.fingerprint))) }

    static func channel(host: Host) -> ChannelGrant? { read(channelKey(host8(host.fingerprint))) }
    static func setChannel(_ channel: ChannelGrant, host: Host) { write(channel, channelKey(host8(host.fingerprint))) }

    /// The names of the numbers, as `island-names-<host8>.json` in the App Group.
    static func saveNames(_ names: IslandNames, host: Host) {
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

    private static func read<T: Decodable>(_ key: String) -> T? {
        guard let data = defaults.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    private static func write<T: Encodable>(_ value: T, _ key: String) {
        guard let data = try? JSONEncoder().encode(value) else { return }
        defaults.set(data, forKey: key)
    }
}
