import Foundation

/// The names the phone keeps for the numbers the island shows; the host never sends them in clear.
public struct IslandNames: Codable, Equatable, Sendable {
    public var sessions: [Int: String]
    public var accounts: [Int: String]

    public init(sessions: [Int: String] = [:], accounts: [Int: String] = [:]) {
        self.sessions = sessions; self.accounts = accounts
    }

    public func session(_ n: Int) -> String { sessions[n] ?? "session \(n)" }
    public func account(_ n: Int) -> String { accounts[n] ?? "account \(n)" }

    /// The file in the App Group for one host.
    public static func fileName(host: String) -> String { "island-names-\(host).json" }

    enum CodingKeys: String, CodingKey { case sessions, accounts }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sessions = try decodeIntKeyed(c, CodingKeys.sessions)
        accounts = try decodeIntKeyed(c, CodingKeys.accounts)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(stringKeyed(sessions), forKey: .sessions)
        try c.encode(stringKeyed(accounts), forKey: .accounts)
    }
}
