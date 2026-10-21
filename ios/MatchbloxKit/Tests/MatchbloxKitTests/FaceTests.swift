import XCTest
@testable import MatchbloxKit

final class FaceTests: XCTestCase {
    let now = Date(timeIntervalSince1970: 1_800_000_000)

    func state(host: String = "f10f7812", stoker: Bool = false, pcts: [Int] = []) -> IslandState {
        IslandState(host: host, burn: 0, wait: 0, top: nil,
                    accounts: [AccountUse(n: 1, windows: pcts.enumerated().map { WindowUse(name: "w\($0.offset)", pct: $0.element, resets: nil) }, lasts: true, runsOut: nil)],
                    stoker: stoker)
    }

    func testAStateOfAHostNotFollowedShowsNothing() {
        let s = state()
        XCTAssertEqual(face(for: s, show: .sessions, followed: "a30534a5", now: now), .nothing)
        XCTAssertEqual(face(for: s, show: .usage, followed: nil, now: now), .nothing)
        XCTAssertEqual(face(for: s, show: .sessions, followed: "f10f7812", now: now), .sessions)
        XCTAssertEqual(face(for: s, show: .usage, followed: "f10f7812", now: now), .usage)
    }

    func testAutoShowsUsageWhileTheStokerRuns() {
        let f = "f10f7812"
        XCTAssertEqual(face(for: state(stoker: true), show: .auto, followed: f, now: now), .usage)
        XCTAssertEqual(face(for: state(stoker: false), show: .auto, followed: f, now: now), .sessions)
    }

    func testAWaitBreaksThroughFor60s() {
        var s = state(stoker: true)
        s.waitAt = now
        let f = "f10f7812"
        XCTAssertEqual(face(for: s, show: .usage, followed: f, now: now), .sessions)
        XCTAssertEqual(face(for: s, show: .auto, followed: f, now: now.addingTimeInterval(59)), .sessions)
        XCTAssertEqual(face(for: s, show: .usage, followed: f, now: now.addingTimeInterval(60)), .usage)
        XCTAssertEqual(face(for: s, show: .usage, followed: "other", now: now), .nothing)
        XCTAssertEqual(breakThroughEnds(s), now.addingTimeInterval(60))
        XCTAssertNil(breakThroughEnds(state()))
    }

    func testTheTightestWindowWins() {
        let s = state(pcts: [31, 93, 71])
        XCTAssertEqual(tightest(s)?.pct, 93)
        XCTAssertNil(tightest(state()))
    }

    func testBarsWarnAt60AndNegAt90() {
        XCTAssertEqual(barColorLevel(0), .normal)
        XCTAssertEqual(barColorLevel(59), .normal)
        XCTAssertEqual(barColorLevel(60), .warn)
        XCTAssertEqual(barColorLevel(89), .warn)
        XCTAssertEqual(barColorLevel(90), .neg)
        XCTAssertEqual(barColorLevel(100), .neg)
    }
}
