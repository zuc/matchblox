import MatchbloxKit
import SwiftUI
import WidgetKit

/// The previews of each size and face, from the content state of testdata/push/content-state.json.
/// In a preview the fixture's host is followed and Show is Auto: a state with the stoker on
/// shows Usage, one without shows Sessions, and a fresh wait breaks through.
enum IslandPreview {
    static var running: Bool { ProcessInfo.processInfo.environment["XCODE_RUNNING_FOR_PREVIEWS"] == "1" }

    static func model(_ state: IslandState, now: Date) -> IslandModel {
        IslandModel(
            state: state,
            face: face(for: state, show: .auto, followed: state.host, now: now),
            names: IslandNames(sessions: [3: "api"], accounts: [1: "Max", 2: "Plus"]),
            label: "ws-1",
            now: now)
    }

    /// testdata/push/content-state.json, verbatim.
    static let fixtureJSON = """
    {
      "host": "f10f7812",
      "burn": 2,
      "wait": 1,
      "wait_at": "2026-10-21T15:00:00Z",
      "top": {"n": 3, "state": "waits", "pct": 45, "out": "2026-10-21T16:40:00Z"},
      "accounts": [
        {"n": 1, "windows": [{"name": "5h", "pct": 62, "resets": "2026-10-21T17:00:00Z"}, {"name": "wk", "pct": 31, "resets": "2026-10-26T09:00:00Z"}], "lasts": true},
        {"n": 2, "windows": [{"name": "5h", "pct": 93, "resets": "2026-10-21T17:00:00Z"}, {"name": "wk", "pct": 71, "resets": "2026-10-26T09:00:00Z"}], "lasts": false, "runs_out": "2026-10-21T16:15:00Z"}
      ],
      "stoker": true
    }
    """

    static let fixture: IslandState = try! IslandJSON.decoder.decode(IslandState.self, from: Data(fixtureJSON.utf8))

    static func sessions(_ state: String, pct: Int = 62, wait: Int = 0) -> IslandState {
        var s = fixture
        s.stoker = false
        s.wait = wait
        s.waitAt = wait > 0 ? s.waitAt : nil
        s.burn = 2
        s.top = .init(n: 3, state: state, pct: pct, out: s.top?.out)
        return s
    }

    static var burns: IslandState { sessions("burns") }
    static var waits: IslandState { sessions("waits", pct: 45, wait: 1) }
    static var done: IslandState { sessions("done", pct: 100) }
    static var out: IslandState { sessions("out", pct: 40) }
    static var quiet: IslandState {
        var s = sessions("quiet")
        s.top = nil
        s.burn = 0
        return s
    }

    /// The fixture: the stoker runs and `api` waits; its wait began 5 minutes ago, so the
    /// break-through is over and the face is Usage, with `╿ 1`.
    static var usage: IslandState {
        var s = fixture
        s.waitAt = Date.now.addingTimeInterval(-300)
        return s
    }

    /// Usage while api burns and nothing waits.
    static var usageBurns: IslandState {
        var s = fixture
        s.wait = 0
        s.waitAt = nil
        s.top = .init(n: 3, state: "burns", pct: 62)
        return s
    }

    /// Usage with nothing on: the split's left is `▰`.
    static var usageNothingOn: IslandState {
        var s = usageBurns
        s.top = nil
        s.burn = 0
        return s
    }

    /// A wait that has just begun breaks through the Usage face.
    static var breakThrough: IslandState {
        var s = fixture
        s.waitAt = .now
        return s
    }
}

#Preview("Lock screen", as: .content, using: IslandAttributes(host: "f10f7812")) {
    MatchbloxIslandActivity()
} contentStates: {
    IslandPreview.burns
    IslandPreview.waits
    IslandPreview.done
    IslandPreview.out
    IslandPreview.quiet
    IslandPreview.usage
    IslandPreview.usageBurns
    IslandPreview.breakThrough
}

#Preview("Compact", as: .dynamicIsland(.compact), using: IslandAttributes(host: "f10f7812")) {
    MatchbloxIslandActivity()
} contentStates: {
    IslandPreview.burns
    IslandPreview.waits
    IslandPreview.out
    IslandPreview.quiet
    IslandPreview.usage
    IslandPreview.usageBurns
    IslandPreview.usageNothingOn
}

#Preview("Expanded", as: .dynamicIsland(.expanded), using: IslandAttributes(host: "f10f7812")) {
    MatchbloxIslandActivity()
} contentStates: {
    IslandPreview.burns
    IslandPreview.waits
    IslandPreview.usage
    IslandPreview.breakThrough
}

#Preview("Minimal", as: .dynamicIsland(.minimal), using: IslandAttributes(host: "f10f7812")) {
    MatchbloxIslandActivity()
} contentStates: {
    IslandPreview.burns
    IslandPreview.waits
    IslandPreview.quiet
    IslandPreview.usage
    IslandPreview.usageBurns
}
