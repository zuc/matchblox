import Foundation

/// JSON as Go writes it: RFC 3339 times with `Z` (with or without a fraction).
/// The island's types read and write their dates as RFC 3339 themselves, so any decoder
/// works, ActivityKit's plain one included; these stay for the other types and tests.
public enum IslandJSON {
    public static var decoder: JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            if let date = RFC3339.date(s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "not RFC 3339: \(s)"))
        }
        return d
    }

    public static var encoder: JSONEncoder {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .custom { date, encoder in
            var c = encoder.singleValueContainer()
            try c.encode(RFC3339.string(date))
        }
        e.outputFormatting = [.sortedKeys]
        return e
    }
}

/// RFC 3339 dates read and written by hand, whatever date strategy a coder has.
enum RFC3339 {
    static func date(_ s: String) -> Date? {
        let plain = ISO8601DateFormatter()
        let frac = ISO8601DateFormatter()
        frac.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return plain.date(from: s) ?? frac.date(from: s)
    }

    /// As Go writes a time: whole seconds without a fraction, else with one.
    static func string(_ d: Date) -> String {
        let f = ISO8601DateFormatter()
        if d.timeIntervalSince1970.rounded(.down) != d.timeIntervalSince1970 {
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        }
        return f.string(from: d)
    }

    /// An RFC 3339 string, or a number of seconds since 1970.
    static func decode<K: CodingKey>(_ c: KeyedDecodingContainer<K>, _ key: K) throws -> Date? {
        guard c.contains(key), try !c.decodeNil(forKey: key) else { return nil }
        if let s = try? c.decode(String.self, forKey: key) {
            guard let d = date(s) else {
                throw DecodingError.dataCorruptedError(forKey: key, in: c, debugDescription: "not RFC 3339: \(s)")
            }
            return d
        }
        return Date(timeIntervalSince1970: try c.decode(Double.self, forKey: key))
    }

    static func encode<K: CodingKey>(_ d: Date?, _ c: inout KeyedEncodingContainer<K>, _ key: K) throws {
        guard let d else { return }
        try c.encode(string(d), forKey: key)
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

        enum CodingKeys: String, CodingKey { case n, state, pct, out }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            n = try c.decode(Int.self, forKey: .n)
            state = try c.decode(String.self, forKey: .state)
            pct = try c.decode(Int.self, forKey: .pct)
            out = try RFC3339.decode(c, .out)
        }

        public func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(n, forKey: .n)
            try c.encode(state, forKey: .state)
            try c.encode(pct, forKey: .pct)
            try RFC3339.encode(out, &c, .out)
        }
    }

    public var host: String
    public var burn: Int
    public var wait: Int
    public var waitAt: Date?  // when the newest wait began
    public var top: Top?
    public var accounts: [AccountUse]
    public var stoker: Bool

    enum CodingKeys: String, CodingKey {
        case host, burn, wait, top, accounts, stoker
        case waitAt = "wait_at"
    }

    public init(host: String, burn: Int, wait: Int, waitAt: Date? = nil, top: Top?, accounts: [AccountUse], stoker: Bool) {
        self.host = host; self.burn = burn; self.wait = wait; self.waitAt = waitAt
        self.top = top; self.accounts = accounts; self.stoker = stoker
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.decode(String.self, forKey: .host)
        burn = try c.decode(Int.self, forKey: .burn)
        wait = try c.decode(Int.self, forKey: .wait)
        waitAt = try RFC3339.decode(c, .waitAt)
        top = try c.decodeIfPresent(Top.self, forKey: .top)
        accounts = try c.decodeIfPresent([AccountUse].self, forKey: .accounts) ?? []
        stoker = try c.decode(Bool.self, forKey: .stoker)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(host, forKey: .host)
        try c.encode(burn, forKey: .burn)
        try c.encode(wait, forKey: .wait)
        try RFC3339.encode(waitAt, &c, .waitAt)
        try c.encodeIfPresent(top, forKey: .top)
        try c.encode(accounts, forKey: .accounts)
        try c.encode(stoker, forKey: .stoker)
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

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        n = try c.decode(Int.self, forKey: .n)
        windows = try c.decodeIfPresent([WindowUse].self, forKey: .windows) ?? []
        lasts = try c.decode(Bool.self, forKey: .lasts)
        runsOut = try RFC3339.decode(c, .runsOut)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(n, forKey: .n)
        try c.encode(windows, forKey: .windows)
        try c.encode(lasts, forKey: .lasts)
        try RFC3339.encode(runsOut, &c, .runsOut)
    }
}

public struct WindowUse: Codable, Hashable, Sendable {
    public var name: String  // 5h, wk or mo
    public var pct: Int
    public var resets: Date?

    public init(name: String, pct: Int, resets: Date? = nil) {
        self.name = name; self.pct = pct; self.resets = resets
    }

    enum CodingKeys: String, CodingKey { case name, pct, resets }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        pct = try c.decode(Int.self, forKey: .pct)
        resets = try RFC3339.decode(c, .resets)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(name, forKey: .name)
        try c.encode(pct, forKey: .pct)
        try RFC3339.encode(resets, &c, .resets)
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
