import Foundation

/// JSON as Go writes it: RFC 3339 times with `Z` (with or without a fraction).
public enum IslandJSON {
    public static var decoder: JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            let plain = ISO8601DateFormatter()
            let frac = ISO8601DateFormatter()
            frac.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = plain.date(from: s) ?? frac.date(from: s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "not RFC 3339: \(s)"))
        }
        return d
    }

    public static var encoder: JSONEncoder {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .custom { date, encoder in
            var c = encoder.singleValueContainer()
            try c.encode(ISO8601DateFormatter().string(from: date))
        }
        e.outputFormatting = [.sortedKeys]
        return e
    }
}

/// What the Live Activity shows; mirrors Go push.ContentState.
public struct IslandState: Codable, Hashable, Sendable {
    public struct Top: Codable, Hashable, Sendable {
        public var n: Int
        public var state: String  // burns, waits, done, out or quiet
        public var pct: Int
        public var out: Date?

        public init(n: Int, state: String, pct: Int, out: Date? = nil) {
            self.n = n; self.state = state; self.pct = pct; self.out = out
        }
    }

    public var host: String
    public var burn: Int
    public var wait: Int
    public var top: Top?
    public var accounts: [AccountUse]
    public var stoker: Bool

    public init(host: String, burn: Int, wait: Int, top: Top?, accounts: [AccountUse], stoker: Bool) {
        self.host = host; self.burn = burn; self.wait = wait
        self.top = top; self.accounts = accounts; self.stoker = stoker
    }
}

public struct AccountUse: Codable, Hashable, Sendable {
    public var n: Int
    public var windows: [WindowUse]
    public var lasts: Bool
    public var runsOut: Date?

    public init(n: Int, windows: [WindowUse], lasts: Bool, runsOut: Date? = nil) {
        self.n = n; self.windows = windows; self.lasts = lasts; self.runsOut = runsOut
    }

    enum CodingKeys: String, CodingKey {
        case n, windows, lasts
        case runsOut = "runs_out"
    }
}

public struct WindowUse: Codable, Hashable, Sendable {
    public var name: String  // 5h, wk or mo
    public var pct: Int
    public var resets: Date?

    public init(name: String, pct: Int, resets: Date? = nil) {
        self.name = name; self.pct = pct; self.resets = resets
    }
}

/// The buzz event; mirrors Go push.Buzz. `sealed` is enc (32) followed by the ciphertext.
public struct BuzzEvent: Codable, Hashable, Sendable {
    public var host: String
    public var n: Int
    public var sealed: Data

    public init(host: String, n: Int, sealed: Data) {
        self.host = host; self.n = n; self.sealed = sealed
    }
}

public struct IslandReg: Codable, Hashable, Sendable {
    public var startToken: String
    public var channel: String

    public init(startToken: String, channel: String) {
        self.startToken = startToken; self.channel = channel
    }

    enum CodingKeys: String, CodingKey {
        case startToken = "start_token"
        case channel
    }
}

public struct RegisterRequest: Codable, Hashable, Sendable {
    public var deviceToken: String
    public var grants: [String: String]
    public var notifyKey: Data
    public var buzz: Bool
    public var island: IslandReg?

    public init(deviceToken: String, grants: [String: String], notifyKey: Data, buzz: Bool, island: IslandReg? = nil) {
        self.deviceToken = deviceToken; self.grants = grants; self.notifyKey = notifyKey
        self.buzz = buzz; self.island = island
    }

    enum CodingKeys: String, CodingKey {
        case deviceToken = "device_token"
        case grants
        case notifyKey = "notify_key"
        case buzz, island
    }
}

/// Go marshals `map[int]string` with string keys; Swift would write an array.
func decodeIntKeyed<K: CodingKey>(_ c: KeyedDecodingContainer<K>, _ key: K) throws -> [Int: String] {
    guard let raw = try c.decodeIfPresent([String: String].self, forKey: key) else { return [:] }
    var out: [Int: String] = [:]
    for (k, v) in raw {
        guard let n = Int(k) else {
            throw DecodingError.dataCorruptedError(forKey: key, in: c, debugDescription: "key \(k) is not a number")
        }
        out[n] = v
    }
    return out
}

func stringKeyed(_ m: [Int: String]) -> [String: String] {
    Dictionary(uniqueKeysWithValues: m.map { (String($0.key), $0.value) })
}

public struct RegisterReply: Codable, Hashable, Sendable {
    public var ok: Bool
    public var error: String?
    public var sessions: [Int: String]
    public var accounts: [Int: String]

    public init(ok: Bool, error: String? = nil, sessions: [Int: String] = [:], accounts: [Int: String] = [:]) {
        self.ok = ok; self.error = error; self.sessions = sessions; self.accounts = accounts
    }

    enum CodingKeys: String, CodingKey { case ok, error, sessions, accounts }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.decode(Bool.self, forKey: .ok)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        sessions = try decodeIntKeyed(c, CodingKeys.sessions)
        accounts = try decodeIntKeyed(c, CodingKeys.accounts)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(ok, forKey: .ok)
        try c.encodeIfPresent(error, forKey: .error)
        try c.encode(stringKeyed(sessions), forKey: .sessions)
        try c.encode(stringKeyed(accounts), forKey: .accounts)
    }
}

/// The first 8 hex characters of the SHA-256 in "SHA256:<base64>", lowercase; "" if it is not one.
public func host8(_ fingerprint: String) -> String {
    guard fingerprint.hasPrefix("SHA256:") else { return "" }
    var b64 = String(fingerprint.dropFirst("SHA256:".count))
    while b64.hasSuffix("=") { b64.removeLast() }
    while b64.count % 4 != 0 { b64 += "=" }
    guard let sum = Data(base64Encoded: b64), sum.count >= 4 else { return "" }
    return sum.prefix(4).map { String(format: "%02x", $0) }.joined()
}
