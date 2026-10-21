import Foundation

/// The state of the top session as the island draws it (design 0006 §6.6).
public enum TopState: String, Equatable, Sendable {
    case burns, waits, done, out, quiet
}

/// The words and numbers the two faces show, as plain values; the widget only draws them.
public enum IslandLook {
    /// What the match draws: its state and where the front of the burn sits (0...1).
    public struct Match: Equatable, Sendable {
        public var state: TopState
        public var front: Double
        public init(state: TopState, front: Double) { self.state = state; self.front = front }
    }

    /// The state of the top session; no top session, or a state the phone does not know, is quiet.
    public static func state(_ top: IslandState.Top?) -> TopState {
        guard let top else { return .quiet }
        return TopState(rawValue: top.state) ?? .quiet
    }

    /// The match of the top session. Done is burnt to the end; a session with no progress
    /// keeps the flame near the head and the wood whole.
    public static func match(_ top: IslandState.Top?) -> Match {
        let s = state(top)
        let pct = Double(min(max(top?.pct ?? 0, 0), 100)) / 100
        return Match(state: s, front: s == .done ? 1 : max(0.03, pct))
    }

    /// Whether anything is on: a top session that is not quiet. Nothing on, the Usage
    /// compact shows the host mark `▰` where the match would be.
    public static func anythingOn(_ s: IslandState) -> Bool { state(s.top) != .quiet }

    /// The word at the right of the lock screen's last line.
    public static func word(_ top: IslandState.Top?, zone: TimeZone = .current) -> String {
        switch state(top) {
        case .burns: "✦ burns"
        case .waits: "╿ waits"
        case .done: "╿ done"
        case .out: top?.out.map { "out until \(clock($0, zone: zone))" } ?? "out"
        case .quiet: ""
        }
    }

    /// The right of the Sessions compact island.
    public static func compact(_ top: IslandState.Top?, names: IslandNames, zone: TimeZone = .current) -> String {
        switch state(top) {
        case .burns: names.session(top!.n)
        case .waits: "╿ waits"
        case .done: "╿ done"
        case .out: top?.out.map { "out \(clock($0, zone: zone))" } ?? "out"
        case .quiet: "quiet"
        }
    }

    /// The name and what it does, for the lock screen. No percent on the lock screen: the match is the number.
    public static func who(_ top: IslandState.Top?, names: IslandNames) -> (name: String, detail: String?) {
        switch state(top) {
        case .burns: (names.session(top!.n), nil)
        case .waits: (names.session(top!.n), "waits for you")
        case .done: (names.session(top!.n), "finished its turn")
        case .out: (names.session(top!.n), "the limit stopped it")
        case .quiet: ("All quiet", nil)
        }
    }

    /// The same for the expanded island, which shows the percent of a burning session.
    public static func expandedWho(_ top: IslandState.Top?, names: IslandNames) -> (name: String, detail: String?) {
        if state(top) == .burns, let top { return (names.session(top.n), "\(top.pct)%") }
        return who(top, names: names)
    }

    /// `Answer` for a session that waits, `Open` for the rest.
    public static func button(_ top: IslandState.Top?) -> String { state(top) == .waits ? "Answer" : "Open" }

    /// When a content state goes stale: the end of a wait's break-through, while it is
    /// still ahead. The system draws the activity again then, and the Usage face comes back.
    public static func staleDate(_ s: IslandState, now: Date) -> Date? {
        guard let end = breakThroughEnds(s), end > now else { return nil }
        return end
    }

    /// Where a tap and the button go: the top session of this host, if any.
    public static func link(_ s: IslandState) -> URL? {
        guard let top = s.top else { return nil }
        return OpenLink(host: s.host, session: top.n)?.url
    }

    // MARK: usage

    /// The bar of the console's dock: 8 cells, `▰` filled and `▱` empty.
    public static func cells(_ pct: Int) -> (filled: Int, empty: Int) {
        let n = min(8, max(0, Int((Double(pct) / 12.5).rounded())))
        return (n, 8 - n)
    }

    /// The right of the Usage compact island: `╿` while anything waits, and the tightest window.
    public static func usageCompact(_ s: IslandState) -> (waits: Bool, text: String, pct: Int)? {
        guard let w = tightest(s) else { return nil }
        return (s.wait > 0, "\(w.name) \(w.pct)%", w.pct)
    }

    /// The accounts a face shows: the expanded island and the lock screen have room for 2.
    public static func shown(_ s: IslandState) -> [AccountUse] { Array(s.accounts.prefix(2)) }

    /// The forecast of one account: `lasts the week` (muted) or `out Fri ~20:00` (warn).
    public static func forecast(_ a: AccountUse, now: Date, zone: TimeZone = .current) -> (text: String, out: Bool)? {
        if a.lasts { return ("lasts the week", false) }
        guard let at = a.runsOut else { return nil }
        return ("out \(when(at, now: now, zone: zone, approx: true))", true)
    }

    /// When a window resets: the time on the same day, else the weekday and the time.
    public static func resets(_ w: WindowUse, now: Date, zone: TimeZone = .current) -> String? {
        w.resets.map { when($0, now: now, zone: zone, approx: false) }
    }

    static func when(_ date: Date, now: Date, zone: TimeZone, approx: Bool) -> String {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = zone
        let t = (approx ? "~" : "") + clock(date, zone: zone)
        guard !cal.isDate(date, inSameDayAs: now) else { return t }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = zone
        f.dateFormat = "EEE"
        return "\(f.string(from: date)) \(t)"
    }

    static func clock(_ date: Date, zone: TimeZone) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = zone
        f.dateFormat = "HH:mm"
        return f.string(from: date)
    }
}

/// Where the app keeps the island's setting and names for the widget, by name.
public enum IslandShelf {
    /// The App Group UserDefaults key of the encoded `IslandPrefs`.
    public static let prefsKey = "island-prefs"
    /// The App Group UserDefaults key of a host's short name, by its 8 characters.
    public static func labelKey(host: String) -> String { "island-label-\(host)" }

    /// The followed host's 8 characters, which a content state's `host` is compared with.
    public static func followed(_ prefs: IslandPrefs) -> String? { prefs.follow.map(host8) }
}

/// The notify key in the shared keychain group: the app makes it, the buzz extension opens with it.
public enum NotifyKeyItem {
    public static let service = "com.blox.matchblox.notify"
    public static let account = "notify-key"
}

/// A buzz as the Notification Service Extension gets it (internal/push/sender.go buzzBody):
/// `{"aps":{...,"mutable-content":1},"buzz":{"host":"f10f7812","n":3,"sealed":"<base64>"}}`.
public enum BuzzPayload {
    /// The buzz event of a notification's userInfo, or nil if it is not one.
    public static func event(_ userInfo: [AnyHashable: Any]) -> BuzzEvent? {
        guard let b = userInfo["buzz"] as? [String: Any],
              let host = b["host"] as? String,
              let n = b["n"] as? Int,
              let s = b["sealed"] as? String, let sealed = Data(base64Encoded: s)
        else { return nil }
        return BuzzEvent(host: host, n: n, sealed: sealed)
    }

    /// The line to show and the link a tap opens; nil when anything fails, and the
    /// placeholder alert goes as it came (never the ciphertext).
    public static func open(_ userInfo: [AnyHashable: Any], open: (Data) throws -> String) -> (body: String, link: URL)? {
        guard let e = event(userInfo), let link = OpenLink(host: e.host, session: e.n),
              let line = try? open(e.sealed), !line.isEmpty
        else { return nil }
        return (line, link.url)
    }
}
