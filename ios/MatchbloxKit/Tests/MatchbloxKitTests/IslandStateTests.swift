import XCTest
@testable import MatchbloxKit

final class IslandStateTests: XCTestCase {
    func testTheStateReadsTheFixture() throws {
        let s: IslandState = try PushFixture.load("content-state")
        XCTAssertEqual(s.host, "f10f7812")
        XCTAssertEqual(s.burn, 2)
        XCTAssertEqual(s.wait, 1)
        XCTAssertEqual(s.waitAt, ISO8601DateFormatter().date(from: "2026-10-21T15:00:00Z"))
        XCTAssertEqual(s.top?.n, 3)
        XCTAssertEqual(s.top?.state, "waits")
        XCTAssertEqual(s.top?.pct, 45)
        XCTAssertEqual(s.top?.out, ISO8601DateFormatter().date(from: "2026-10-21T16:40:00Z"))
        XCTAssertEqual(s.accounts.count, 2)
        XCTAssertEqual(s.accounts[0].windows.map(\.name), ["5h", "wk"])
        XCTAssertNil(s.accounts[0].runsOut)
        XCTAssertTrue(s.accounts[0].lasts)
        XCTAssertNotNil(s.accounts[1].runsOut)
        XCTAssertTrue(s.stoker)
        let again = try IslandJSON.decoder.decode(IslandState.self, from: IslandJSON.encoder.encode(s))
        XCTAssertEqual(again, s)
    }

    func testTheRegisterTypesReadTheFixtures() throws {
        let r: RegisterRequest = try PushFixture.load("register-request")
        // Grants are keyed as the host's Sender looks them up: "<kind>:<target>".
        XCTAssertEqual(r.grants[Relay.grantKey("device", r.deviceToken)], "Z3JhbnQtb25l")
        XCTAssertEqual(r.grants[Relay.grantKey("start", "6162636465666768")], "Z3JhbnQtdHdv")
        XCTAssertEqual(r.grants[Relay.grantKey("channel", "Y2hhbm5lbC0x")], "Z3JhbnQtdGhyZWU=")
        XCTAssertEqual(r.notifyKey.count, 32)
        XCTAssertTrue(r.buzz)
        XCTAssertEqual(r.island?.startToken, "6162636465666768")
        let again = try IslandJSON.decoder.decode(RegisterRequest.self, from: IslandJSON.encoder.encode(r))
        XCTAssertEqual(again, r)
        let p: RegisterReply = try PushFixture.load("register-reply")
        XCTAssertTrue(p.ok)
        XCTAssertEqual(p.sessions[3], "e5f6a7b8")
        XCTAssertEqual(p.accounts[2], "0a1b2c3d")
        let b: BuzzEvent = try PushFixture.load("buzz")
        XCTAssertEqual(b.n, 3)
        XCTAssertEqual(b.sealed.count, 32)
    }

    func testHost8MatchesGo() throws {
        struct Row: Decodable { let fingerprint: String; let host8: String }
        let rows: [Row] = try PushFixture.load("host8")
        XCTAssertFalse(rows.isEmpty)
        for r in rows { XCTAssertEqual(host8(r.fingerprint), r.host8) }
        XCTAssertEqual(host8("nope"), "")
    }
}
