import CryptoKit
import XCTest
@testable import MatchbloxKit

final class IslandLookTests: XCTestCase {
    let utc = TimeZone(identifier: "UTC")!
    let names = IslandNames(sessions: [3: "api"], accounts: [1: "Max"])

    func fixture() throws -> IslandState { try PushFixture.load("content-state") }

    func top(_ state: String, pct: Int = 45, out: Date? = nil) -> IslandState.Top {
        IslandState.Top(n: 3, state: state, pct: pct, out: out)
    }

    func testEachStateHasItsWordsAndMatch() {
        let out = Date(timeIntervalSince1970: 1_792_600_800)  // 2026-10-21 16:40 UTC
        XCTAssertEqual(IslandLook.word(top("burns"), zone: utc), "✦ burns")
        XCTAssertEqual(IslandLook.word(top("waits"), zone: utc), "╿ waits")
        XCTAssertEqual(IslandLook.word(top("done"), zone: utc), "╿ done")
        XCTAssertEqual(IslandLook.word(top("out", out: out), zone: utc), "out until 16:40")
        XCTAssertEqual(IslandLook.word(nil, zone: utc), "")
        XCTAssertEqual(IslandLook.compact(top("burns"), names: names), "api")
        XCTAssertEqual(IslandLook.compact(top("out", out: out), names: names, zone: utc), "out 16:40")
        XCTAssertEqual(IslandLook.compact(nil, names: names), "quiet")
        XCTAssertEqual(IslandLook.match(top("burns", pct: 62)), .init(state: .burns, front: 0.62))
        XCTAssertEqual(IslandLook.match(top("burns", pct: 0)), .init(state: .burns, front: 0.03))
        XCTAssertEqual(IslandLook.match(top("done", pct: 40)).front, 1)
        XCTAssertEqual(IslandLook.match(top("sleeps")).state, .quiet)
        XCTAssertEqual(IslandLook.match(nil).state, .quiet)
    }

    func testTheLockScreenHasNoPercentButTheExpandedHas() {
        XCTAssertNil(IslandLook.who(top("burns", pct: 62), names: names).detail)
        XCTAssertEqual(IslandLook.expandedWho(top("burns", pct: 62), names: names).detail, "62%")
        XCTAssertEqual(IslandLook.who(top("waits"), names: names).detail, "waits for you")
        XCTAssertEqual(IslandLook.who(nil, names: names).name, "All quiet")
    }

    func testAnUnknownNumberShowsItsNumber() {
        let t = IslandState.Top(n: 12, state: "waits", pct: 0)
        XCTAssertEqual(IslandLook.who(t, names: names).name, "session 12")
        XCTAssertEqual(names.account(3), "account 3")
    }

    func testTheStateGoesStaleWhenTheBreakThroughEnds() throws {
        let s = try fixture()  // wait_at 15:00
        let at = try XCTUnwrap(s.waitAt)
        XCTAssertEqual(IslandLook.staleDate(s, now: at.addingTimeInterval(10)), at.addingTimeInterval(60))
        XCTAssertNil(IslandLook.staleDate(s, now: at.addingTimeInterval(60)))
        var none = s
        none.waitAt = nil
        XCTAssertNil(IslandLook.staleDate(none, now: at))
    }

    func testAWaitingSessionIsAnswered() {
        XCTAssertEqual(IslandLook.button(top("waits")), "Answer")
        XCTAssertEqual(IslandLook.button(top("burns")), "Open")
        XCTAssertEqual(IslandLook.button(nil), "Open")
    }

    func testTheLinkOpensTheTopSession() throws {
        let s = try fixture()
        XCTAssertEqual(IslandLook.link(s)?.absoluteString, "matchblox://open?host=f10f7812&session=3")
        var none = s
        none.top = nil
        XCTAssertNil(IslandLook.link(none))
    }

    func testTheUsageCompactShowsTheTightestAndTheWait() throws {
        var s = try fixture()
        let c = try XCTUnwrap(IslandLook.usageCompact(s))
        XCTAssertTrue(c.waits)
        XCTAssertEqual(c.text, "5h 93%")
        XCTAssertTrue(IslandLook.anythingOn(s))
        s.wait = 0
        s.top = nil
        XCTAssertEqual(IslandLook.usageCompact(s)?.waits, false)
        XCTAssertFalse(IslandLook.anythingOn(s))
        s.accounts = []
        XCTAssertNil(IslandLook.usageCompact(s))
    }

    func testBarsHaveEightCells() {
        XCTAssertEqual(IslandLook.cells(0).filled, 0)
        XCTAssertEqual(IslandLook.cells(61).filled, 5)
        XCTAssertEqual(IslandLook.cells(92).filled, 7)
        XCTAssertEqual(IslandLook.cells(100).filled, 8)
        XCTAssertEqual(IslandLook.cells(140).empty, 0)
    }

    func testForecastAndResets() throws {
        let s = try fixture()
        let now = Date(timeIntervalSince1970: 1_792_594_800)  // 2026-10-21 15:00 UTC
        XCTAssertEqual(IslandLook.forecast(s.accounts[0], now: now, zone: utc)?.text, "lasts the week")
        let out = try XCTUnwrap(IslandLook.forecast(s.accounts[1], now: now, zone: utc))
        XCTAssertEqual(out.text, "out ~16:15")
        XCTAssertTrue(out.out)
        XCTAssertEqual(IslandLook.resets(s.accounts[0].windows[0], now: now, zone: utc), "17:00")
        XCTAssertEqual(IslandLook.resets(s.accounts[0].windows[1], now: now, zone: utc), "Mon 09:00")
        XCTAssertEqual(IslandLook.shown(s).count, 2)
    }

    func testTheFollowedHostIsComparedByItsEightCharacters() throws {
        let pairs = try JSONDecoder().decode([[String: String]].self, from: PushFixture.data("host8"))
        for p in pairs {
            XCTAssertEqual(IslandShelf.followed(IslandPrefs(choice: .host(p["fingerprint"]!))), p["host8"])
        }
        XCTAssertNil(IslandShelf.followed(IslandPrefs(choice: FollowChoice.none)))
    }

    // MARK: the buzz

    struct Vector: Decodable {
        let privateKey: Data
        let plain: String
        let sealed: Data
        enum CodingKeys: String, CodingKey { case privateKey = "private_key", plain, sealed }
    }

    func testTheBuzzPayloadParses() throws {
        let raw = try PushFixture.data("buzz")
        let userInfo: [AnyHashable: Any] = ["aps": ["mutable-content": 1], "buzz": try JSONSerialization.jsonObject(with: raw)]
        let e = try XCTUnwrap(BuzzPayload.event(userInfo))
        XCTAssertEqual(e.host, "f10f7812")
        XCTAssertEqual(e.n, 3)
        XCTAssertEqual(e.sealed.count, 32)
        XCTAssertNil(BuzzPayload.event(["aps": [:]]))
        XCTAssertNil(BuzzPayload.event(["buzz": ["host": "f10f7812", "n": 3, "sealed": "not base64!"]]))
    }

    func testTheSealedBuzzOpensToItsLineAndLink() throws {
        let v: Vector = try PushFixture.load("seal-vector")
        let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: v.privateKey)
        let userInfo: [AnyHashable: Any] = ["buzz": ["host": "f10f7812", "n": 3, "sealed": v.sealed.base64EncodedString()]]
        let got = try XCTUnwrap(BuzzPayload.open(userInfo) { try openBuzz($0, key: key) })
        XCTAssertEqual(got.body, v.plain)
        XCTAssertEqual(got.link.absoluteString, "matchblox://open?host=f10f7812&session=3")
    }

    func testABuzzThatDoesNotOpenKeepsThePlaceholder() throws {
        let v: Vector = try PushFixture.load("seal-vector")
        let other = Curve25519.KeyAgreement.PrivateKey()
        let userInfo: [AnyHashable: Any] = ["buzz": ["host": "f10f7812", "n": 3, "sealed": v.sealed.base64EncodedString()]]
        XCTAssertNil(BuzzPayload.open(userInfo) { try openBuzz($0, key: other) })
        let badHost: [AnyHashable: Any] = ["buzz": ["host": "XYZ", "n": 3, "sealed": v.sealed.base64EncodedString()]]
        let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: v.privateKey)
        XCTAssertNil(BuzzPayload.open(badHost) { try openBuzz($0, key: key) })
    }
}
