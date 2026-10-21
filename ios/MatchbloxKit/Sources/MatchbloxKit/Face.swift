import Foundation

public enum Show: String, Codable, Sendable { case sessions, usage, auto }
public enum Face: Equatable, Sendable { case nothing, sessions, usage }
public enum Level: Equatable, Sendable { case normal, warn, neg }

/// Which face the island shows. One island at a time: nothing for a host that is not followed.
/// A wait breaks through the Usage face until `breakThroughUntil` (the caller sets now + 60 s).
public func face(for s: IslandState, show: Show, followed: String?, breakThroughUntil: Date?, now: Date) -> Face {
    guard let followed, s.host == followed else { return .nothing }
    let wanted: Face
    switch show {
    case .sessions: wanted = .sessions
    case .usage: wanted = .usage
    case .auto: wanted = s.stoker ? .usage : .sessions
    }
    if wanted == .usage, let until = breakThroughUntil, now < until { return .sessions }
    return wanted
}

/// The window with the highest percent over all accounts.
public func tightest(_ s: IslandState) -> WindowUse? {
    s.accounts.flatMap(\.windows).max { $0.pct < $1.pct }
}

/// Bars warn from 60 %, neg from 90 % (DESIGN.md section 6 Dock).
public func barColorLevel(_ pct: Int) -> Level {
    pct >= 90 ? .neg : pct >= 60 ? .warn : .normal
}
