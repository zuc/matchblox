import ActivityKit
import CryptoKit
import Foundation
import LocalAuthentication
import MatchbloxKit
import NIOSSH
import UIKit
import UserNotifications

/// The tokens iOS gives the app: the APNs device token (the buzz) and the push-to-start
/// token of the island. Both arrive some time after launch.
@MainActor
final class PushTokens {
    static let shared = PushTokens()

    /// The device token: the one kept from before until iOS gives this launch's.
    var device: String? = IslandSettings.keptDeviceToken
    var start: String?
    private var watching = false

    /// At launch: asks iOS for both tokens. Nothing here connects to a host.
    func begin() {
        guard !watching else { return }
        watching = true
        UIApplication.shared.registerForRemoteNotifications()
        Task {
            for await data in Activity<IslandAttributes>.pushToStartTokenUpdates {
                let token = hexToken(data)
                PushTokens.shared.start = token
                IslandSettings.keptStartToken = token
            }
        }
    }

    /// Waits a little for a token that has not come yet.
    func wait(_ token: KeyPath<PushTokens, String?>, for limit: Duration = .seconds(3)) async -> String? {
        let until = ContinuousClock.now + limit
        while self[keyPath: token] == nil, ContinuousClock.now < until {
            try? await Task.sleep(for: .milliseconds(100))
        }
        return self[keyPath: token]
    }
}

/// The X25519 key the buzz is sealed to. Made once, kept in the shared keychain group, where
/// the buzz extension opens buzzes with it.
enum NotifyKey {
    static let group = Bundle.main.object(forInfoDictionaryKey: "MatchbloxKeychainGroup") as? String
    private static let service = "com.blox.matchblox.notify"
    private static let account = "notify-key"

    static func publicKey() -> Data? {
        if let raw = Keychain.get(service: service, account: account, group: group),
           let key = try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: raw) {
            return key.publicKey.rawRepresentation
        }
        let key = Curve25519.KeyAgreement.PrivateKey()
        guard Keychain.set(key.rawRepresentation, service: service, account: account, group: group) else { return nil }
        return key.publicKey.rawRepresentation
    }
}

/// Registers the phone with a host for the buzz and, for the followed host, the island
/// (design 0010 §3.2). It runs only in the foreground, with a key that Face ID or the
/// passcode unlocked.
@MainActor
enum Registration {
    enum Failure: Error, CustomStringConvertible {
        case noDeviceToken, noNotifyKey, noGrant(String)

        var description: String {
            switch self {
            case .noDeviceToken: "iOS gave no device token yet"
            case .noNotifyKey: "the notification key could not be kept"
            case .noGrant(let kind): "the relay gave no grant for \(kind)"
            }
        }
    }

    private static let replyDeadline: Duration = .seconds(20)
    private static var asked = false

    /// The registration the phone would send this host now; nil with no device token.
    /// The channel is the one kept for this host, or "" when the relay has to make one.
    static func desired(for host: Host) async -> RegistrationRecord? {
        await askForAlerts()
        guard let device = await PushTokens.shared.wait(\.device),
              let notifyKey = NotifyKey.publicKey()
        else { return nil }
        let prefs = IslandSettings.prefs
        let followed = prefs.follow == host.fingerprint
        let island = RegistrationRecord.island(
            followed: followed,
            start: followed ? await PushTokens.shared.wait(\.start) : nil,
            kept: IslandSettings.keptStartToken,
            last: IslandSettings.record(host: host)?.island,
            channel: IslandSettings.channel(host: host)?.channel
        )
        return RegistrationRecord(deviceToken: device, notifyKey: notifyKey, buzz: prefs.buzzOn(host.fingerprint), island: island)
    }

    static func isNeeded(_ host: Host) async -> Bool {
        guard let want = await desired(for: host) else { return false }
        return RegistrationRecord.needed(last: IslandSettings.record(host: host), now: want)
    }

    /// Registers with one host when anything changed since the last registration with it.
    /// A host that is not followed gets no island, which clears it there.
    static func run(host: Host, key: NIOSSHPrivateKey) async throws {
        guard var want = await desired(for: host) else { throw Failure.noDeviceToken }
        guard RegistrationRecord.needed(last: IslandSettings.record(host: host), now: want) else { return }
        try Task.checkCancellation()

        var grants = try await Attest.grants(items: Relay.items(deviceToken: want.deviceToken, startToken: want.island?.startToken))
        for item in Relay.items(deviceToken: want.deviceToken, startToken: want.island?.startToken)
        where grants[Relay.grantKey(item.kind, item.value)] == nil {
            throw Failure.noGrant(item.kind)
        }
        try Task.checkCancellation()
        if let island = want.island {
            let channel: ChannelGrant
            if let kept = IslandSettings.channel(host: host), kept.channel == island.channel, !island.channel.isEmpty {
                channel = kept
            } else {
                channel = try await Attest.channel()
                IslandSettings.setChannel(channel, host: host)
            }
            want.island?.channel = channel.channel
            grants[Relay.grantKey("channel", channel.channel)] = channel.grant
        }
        try Task.checkCancellation()

        let request = RegisterRequest(
            deviceToken: want.deviceToken,
            grants: grants,
            notifyKey: want.notifyKey,
            buzz: want.buzz,
            island: want.island
        )
        let reply = try await exchange(request, host: host, key: key)
        IslandSettings.saveNames(IslandNames(sessions: reply.sessions, accounts: reply.accounts), host: host)
        IslandSettings.setRecord(want, host: host)
        islandLog.info("registered with \(host.label, privacy: .public)")
    }

    /// After a console opens and its key is unlocked: the opened host, and the host that the
    /// default Follow (the host opened last) moved away from, with the same unlock. Errors are
    /// logged and never block the console.
    static func afterOpen(host: Host, key: NIOSSHPrivateKey, context: LAContext?, hosts: [Host]) async {
        var prefs = IslandSettings.prefs
        let lost = prefs.opened(host.fingerprint)
        IslandSettings.prefs = prefs
        if lost != nil { IslandSettings.endStrayActivities() }
        do {
            try await run(host: host, key: key)
        } catch {
            islandLog.error("register with \(host.label, privacy: .public): \(String(describing: error), privacy: .public)")
        }
        guard !Task.isCancelled, let lost, let context, let old = hosts.first(where: { $0.fingerprint == lost }) else { return }
        do {
            let oldKey = DeviceKey.nioKey(try DeviceKey.load(host: old.fingerprint, context: context))
            try await run(host: old, key: oldKey)
        } catch {
            islandLog.error("register with \(old.label, privacy: .public): \(String(describing: error), privacy: .public)")
        }
    }

    /// After a change of Follow or Buzz: one Face ID for all the hosts concerned. Answers the
    /// hosts that could not be reached.
    static func afterChange(hosts concerned: [Host]) async -> [Host] {
        IslandSettings.endStrayActivities()
        var due: [Host] = []
        for host in concerned where await isNeeded(host) { due.append(host) }
        guard !due.isEmpty else { return [] }
        let context = LAContext()
        context.touchIDAuthenticationAllowableReuseDuration = 10
        do {
            try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: "Change the island on your hosts.")
        } catch {
            // Still locked: each host changes at its next connect.
            islandLog.info("the island change waits for the next connect")
            return []
        }
        defer { context.invalidate() }
        var unreachable: [Host] = []
        for host in due {
            guard !Task.isCancelled else { break }
            do {
                let key = DeviceKey.nioKey(try DeviceKey.load(host: host.fingerprint, context: context))
                try await run(host: host, key: key)
            } catch let failure as ConnectFailure {
                islandLog.error("register with \(host.label, privacy: .public): \(String(describing: failure), privacy: .public)")
                unreachable.append(host)
            } catch {
                islandLog.error("register with \(host.label, privacy: .public): \(String(describing: error), privacy: .public)")
            }
        }
        return unreachable
    }

    private static func askForAlerts() async {
        guard !asked else { return }
        asked = true
        let center = UNUserNotificationCenter.current()
        guard await center.notificationSettings().authorizationStatus == .notDetermined else { return }
        _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
    }

    /// The ssh exchange closes at once when its task is cancelled (the app went to the background).
    private static func exchange(_ request: RegisterRequest, host: Host, key: NIOSSHPrivateKey) async throws -> RegisterReply {
        let session = SSHSession(host: host.address, port: host.port, user: host.user, key: key, hostKeyFingerprint: host.fingerprint)
        return try await withTaskCancellationHandler {
            try await exchange(request, over: session)
        } onCancel: {
            Task { await session.close() }
        }
    }

    private static func exchange(_ request: RegisterRequest, over session: SSHSession) async throws -> RegisterReply {
        try Task.checkCancellation()
        let output = try await session.open(command: "matchblox push register", pty: nil)
        let deadline = Task {
            try? await Task.sleep(for: replyDeadline)
            await session.close()
        }
        defer {
            deadline.cancel()
            Task { await session.close() }
        }
        try Task.checkCancellation()
        await session.write(request.line)
        let reply = try await RegisterReply.read(from: output)
        try Task.checkCancellation()
        return reply
    }
}
