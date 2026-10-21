import Foundation

/// What the builder picked for Follow. No pick follows the host opened last.
public enum FollowChoice: Codable, Equatable, Sendable {
    case none
    case host(String)  // the host key fingerprint
}

/// The Island setting (design 0010 §4.3), kept in the App Group. Show stays on the
/// phone; Follow and Buzz change what a host sends.
public struct IslandPrefs: Codable, Equatable, Sendable {
    public var choice: FollowChoice?
    public var lastOpened: String?
    public var show: Show
    public var buzz: [String: Bool]

    public init(choice: FollowChoice? = nil, lastOpened: String? = nil, show: Show = .sessions, buzz: [String: Bool] = [:]) {
        self.choice = choice; self.lastOpened = lastOpened; self.show = show; self.buzz = buzz
    }

    /// The fingerprint of the followed host, or nil for `None`. Default: the host opened last.
    public var follow: String? {
        switch choice {
        case nil: lastOpened
        case .none?: nil
        case .host(let fingerprint)?: fingerprint
        }
    }

    /// Buzz is on for each host until it is turned off.
    public func buzzOn(_ fingerprint: String) -> Bool { buzz[fingerprint] ?? true }

    /// Notes the host just opened. Returns the host that stops being followed by it, if any.
    @discardableResult
    public mutating func opened(_ fingerprint: String) -> String? {
        let before = follow
        lastOpened = fingerprint
        guard let before, before != follow else { return nil }
        return before
    }

    /// The hosts whose registration differs between two settings: Follow moved to or from
    /// them, or their Buzz changed. Show changes no host.
    public static func concerned(_ before: IslandPrefs, _ after: IslandPrefs, hosts: [String]) -> [String] {
        hosts.filter { fp in
            (before.follow == fp) != (after.follow == fp) || before.buzzOn(fp) != after.buzzOn(fp)
        }
    }

    /// An activity whose host is not the followed host's 8 characters is a stray: the app ends it.
    public func isStray(activityHost: String) -> Bool {
        guard let follow else { return true }
        return activityHost != host8(follow)
    }
}

/// What the phone last registered with one host. A new registration runs only when the
/// one the phone would send now differs.
public struct RegistrationRecord: Codable, Equatable, Sendable {
    public var deviceToken: String
    public var notifyKey: Data
    public var buzz: Bool
    /// Only for the followed host, and only once iOS has given the push-to-start token.
    public var island: IslandReg?

    public init(deviceToken: String, notifyKey: Data, buzz: Bool, island: IslandReg?) {
        self.deviceToken = deviceToken; self.notifyKey = notifyKey; self.buzz = buzz; self.island = island
    }

    public static func needed(last: RegistrationRecord?, now: RegistrationRecord) -> Bool { last != now }
}

/// A broadcast channel the relay made for one host, with its grant.
public struct ChannelGrant: Codable, Equatable, Sendable {
    public var channel: String
    public var grant: String

    public init(channel: String, grant: String) { self.channel = channel; self.grant = grant }
}

/// The relay's routes (worker/push.js), as plain values.
public enum Relay {
    public static let base = URL(string: "https://matchblox.sh")!
    public static let grantURL = base.appendingPathComponent("push/grant")
    public static let channelURL = base.appendingPathComponent("push/channel")

    public struct Item: Codable, Equatable, Sendable {
        public var kind: String
        public var value: String
        public init(kind: String, value: String) { self.kind = kind; self.value = value }
    }

    /// The items to ask grants for: the device token, and the push-to-start token if any.
    public static func items(deviceToken: String, startToken: String?) -> [Item] {
        var items = [Item(kind: "device", value: deviceToken)]
        if let startToken { items.append(Item(kind: "start", value: startToken)) }
        return items
    }

    /// The client data as the JSON string the relay hashes exactly as sent.
    public static func clientData(items: [Item]?, time: Int) -> String {
        struct Body: Encodable { let items: [Item]?; let time: Int }
        let e = JSONEncoder()
        e.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return String(decoding: try! e.encode(Body(items: items, time: time)), as: UTF8.self)
    }

    /// The key the host looks a grant up by: `<kind>:<target>` (internal/push/sender.go).
    public static func grantKey(_ kind: String, _ target: String) -> String { "\(kind):\(target)" }

    public struct GrantReply: Decodable, Equatable, Sendable {
        public var attestGrant: String
        public var grants: [String: String]
        enum CodingKeys: String, CodingKey { case attestGrant = "attest_grant", grants }
    }

    public struct ChannelReply: Decodable, Equatable, Sendable {
        public var channel: String
        public var grant: String
    }

    public struct Refusal: Decodable, Error, Equatable, Sendable {
        public var error: String?
    }
}

/// An APNs token as the relay and the host take it: lowercase hex.
public func hexToken(_ data: Data) -> String { data.map { String(format: "%02x", $0) }.joined() }

public enum RegisterError: Error, Equatable, Sendable {
    case noReply
    case refused(String)
}

extension RegisterRequest {
    /// The one JSON line `matchblox push register` reads.
    public var line: Data {
        var d = try! JSONEncoder().encode(self)
        d.append(UInt8(ascii: "\n"))
        return d
    }
}

extension RegisterReply {
    /// The first complete line that is a reply. The login shell of the host can print other lines first.
    public static func read(from stream: AsyncStream<Data>, limit: Int = 64 * 1024) async throws(RegisterError) -> RegisterReply {
        var output = Data()
        for await chunk in stream {
            output.append(chunk)
            if let reply = first(in: output, completeOnly: true) { return try checked(reply) }
            if output.count > limit { break }
        }
        guard let reply = first(in: output, completeOnly: false) else { throw .noReply }
        return try checked(reply)
    }

    private static func checked(_ reply: RegisterReply) throws(RegisterError) -> RegisterReply {
        guard reply.ok else { throw .refused(reply.error ?? "refused") }
        return reply
    }

    private static func first(in output: Data, completeOnly: Bool) -> RegisterReply? {
        var lines = output.split(separator: UInt8(ascii: "\n"), omittingEmptySubsequences: false)
        if completeOnly { lines.removeLast() }
        for line in lines {
            let text = String(decoding: line, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
            guard text.hasPrefix("{") else { continue }
            if let reply = try? JSONDecoder().decode(RegisterReply.self, from: Data(text.utf8)) { return reply }
        }
        return nil
    }
}

extension Opening {
    /// A tapped `matchblox://open` link: that host's console on that session; a host this
    /// device does not know opens the hosts list.
    public static func route(for link: OpenLink, hosts: [Host]) -> Route {
        if let host = hosts.first(where: { host8($0.fingerprint) == link.host }) {
            return .console(host, open: link.session)
        }
        return hosts.isEmpty ? .scan : .hosts
    }

    /// The command a console runs: one session when the route came from a link.
    public static func consoleCommand(host: Host, open: Int?) -> String {
        guard let open, let link = OpenLink(host: host8(host.fingerprint), session: open) else { return "matchblox console" }
        return link.command
    }

    public static func notReachable(host: String) -> String {
        "\(host) is not reachable: it changes at the next connect"
    }
}
