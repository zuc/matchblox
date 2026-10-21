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
        XCTAssertEqual(face(for: s, show: .sessions, followed: "a30534a5", breakThroughUntil: nil, now: now), .nothing)
        XCTAssertEqual(face(for: s, show: .usage, followed: nil, breakThroughUntil: nil, now: now), .nothing)
        XCTAssertEqual(face(for: s, show: .sessions, followed: "f10f7812", breakThroughUntil: nil, now: now), .sessions)
        XCTAssertEqual(face(for: s, show: .usage, followed: "f10f7812", breakThroughUntil: nil, now: now), .usage)
    }

    func testAutoShowsUsageWhileTheStokerRuns() {
        let f = "f10f7812"
        XCTAssertEqual(face(for: state(stoker: true), show: .auto, followed: f, breakThroughUntil: nil, now: now), .usage)
        XCTAssertEqual(face(for: state(stoker: false), show: .auto, followed: f, breakThroughUntil: nil, now: now), .sessions)
    }

    func testAWaitBreaksThroughFor60s() {
        let s = state(stoker: true)
        let f = "f10f7812"
        let until = now.addingTimeInterval(60)
        XCTAssertEqual(face(for: s, show: .usage, followed: f, breakThroughUntil: until, now: now), .sessions)
        XCTAssertEqual(face(for: s, show: .auto, followed: f, breakThroughUntil: until, now: now.addingTimeInterval(59)), .sessions)
        XCTAssertEqual(face(for: s, show: .usage, followed: f, breakThroughUntil: until, now: now.addingTimeInterval(60)), .usage)
        XCTAssertEqual(face(for: s, show: .usage, followed: "other", breakThroughUntil: until, now: now), .nothing)
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
