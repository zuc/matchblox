import Foundation
import XCTest
@testable import MatchbloxKit

final class IslandPrefsTests: XCTestCase {
    // host8 of these is f10f7812 and a30534a5 (testdata/push/host8.json style: first 4 bytes).
    private let ws1 = "SHA256:8Q94EgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
    private let work1 = "SHA256:owU0pQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

    private func host(_ label: String, _ fp: String) -> MatchbloxKit.Host {
        Host(label: label, address: "\(label).local", port: 22, user: "dev", fingerprint: fp)
    }

    func testTheFingerprintsGiveTheirHost8() {
        XCTAssertEqual(host8(ws1), "f10f7812")
        XCTAssertEqual(host8(work1), "a30534a5")
    }

    func testTheDefaultsAreSessionsTheHostOpenedLastAndBuzzOn() {
        var p = IslandPrefs()
        XCTAssertEqual(p.show, .sessions)
        XCTAssertNil(p.follow)
        XCTAssertTrue(p.buzzOn(ws1))
        p.opened(ws1)
        XCTAssertEqual(p.follow, ws1)
        XCTAssertEqual(p.opened(work1), ws1, "the default follow moves, so ws-1 loses the island")
        XCTAssertEqual(p.follow, work1)
        XCTAssertNil(p.opened(work1))
    }

    func testAPickedFollowStaysWhenAnotherHostOpens() {
        var p = IslandPrefs(choice: .host(ws1))
        XCTAssertNil(p.opened(work1))
        XCTAssertEqual(p.follow, ws1)
        p.choice = FollowChoice.none
        XCTAssertNil(p.follow)
        XCTAssertNil(p.opened(ws1))
    }

    func testThePrefsRoundTrip() throws {
        let p = IslandPrefs(choice: .host(ws1), lastOpened: work1, show: .auto, buzz: [work1: false])
        let back = try JSONDecoder().decode(IslandPrefs.self, from: JSONEncoder().encode(p))
        XCTAssertEqual(back, p)
        let none = IslandPrefs(choice: FollowChoice.none)
        XCTAssertEqual(try JSONDecoder().decode(IslandPrefs.self, from: JSONEncoder().encode(none)), none)
    }

    func testAChangeOfFollowConcernsTheOldAndTheNewHost() {
        let before = IslandPrefs(choice: .host(ws1))
        var after = before
        after.choice = .host(work1)
        XCTAssertEqual(IslandPrefs.concerned(before, after, hosts: [ws1, work1, "SHA256:other"]), [ws1, work1])
    }

    func testAChangeOfBuzzConcernsThatHostAndShowConcernsNone() {
        let before = IslandPrefs(choice: .host(ws1))
        var after = before
        after.buzz[work1] = false
        XCTAssertEqual(IslandPrefs.concerned(before, after, hosts: [ws1, work1]), [work1])
        var shown = before
        shown.show = .usage
        XCTAssertEqual(IslandPrefs.concerned(before, shown, hosts: [ws1, work1]), [])
    }

    func testStrayActivitiesEndAtLaunch() {
        let p = IslandPrefs(choice: .host(work1))
        XCTAssertTrue(p.isStray(activityHost: "f10f7812"), "ws-1 is no longer followed")
        XCTAssertFalse(p.isStray(activityHost: "a30534a5"))
        XCTAssertTrue(IslandPrefs(choice: FollowChoice.none).isStray(activityHost: "a30534a5"))
    }

    func testARegistrationRunsOnlyWhenSomethingChanged() {
        let reg = RegistrationRecord(deviceToken: "aa", notifyKey: Data([1]), buzz: true, island: nil)
        XCTAssertTrue(RegistrationRecord.needed(last: nil, now: reg))
        XCTAssertFalse(RegistrationRecord.needed(last: reg, now: reg))
        var token = reg; token.deviceToken = "bb"
        XCTAssertTrue(RegistrationRecord.needed(last: reg, now: token))
        var buzz = reg; buzz.buzz = false
        XCTAssertTrue(RegistrationRecord.needed(last: reg, now: buzz))
        var follow = reg; follow.island = IslandReg(startToken: "cc", channel: "ch")
        XCTAssertTrue(RegistrationRecord.needed(last: reg, now: follow))
        var rotated = follow; rotated.island?.startToken = "dd"
        XCTAssertTrue(RegistrationRecord.needed(last: follow, now: rotated))
    }

    func testASlowStartTokenKeepsTheIsland() {
        let last = IslandReg(startToken: "aa", channel: "ch")
        XCTAssertEqual(RegistrationRecord.island(followed: true, start: nil, kept: nil, last: last, channel: "ch"), last)
        XCTAssertEqual(RegistrationRecord.island(followed: true, start: nil, kept: "aa", last: last, channel: "ch"), last)
        XCTAssertEqual(RegistrationRecord.island(followed: true, start: "bb", kept: "aa", last: last, channel: "ch"),
                       IslandReg(startToken: "bb", channel: "ch"))
        XCTAssertNil(RegistrationRecord.island(followed: true, start: nil, kept: nil, last: nil, channel: nil))
        XCTAssertEqual(RegistrationRecord.island(followed: true, start: "bb", kept: nil, last: nil, channel: nil),
                       IslandReg(startToken: "bb", channel: ""))
        XCTAssertNil(RegistrationRecord.island(followed: false, start: "bb", kept: "aa", last: last, channel: "ch"),
                     "only a change of Follow clears the island")
    }

    func testTheClientDataIsTheJSONTheRelayReads() throws {
        let s = Relay.clientData(items: Relay.items(deviceToken: "80b1", startToken: "6162"), time: 1_700_000_000)
        XCTAssertEqual(s, #"{"items":[{"kind":"device","value":"80b1"},{"kind":"start","value":"6162"}],"time":1700000000}"#)
        XCTAssertEqual(Relay.clientData(items: nil, time: 5), #"{"time":5}"#)
        XCTAssertEqual(Relay.items(deviceToken: "80b1", startToken: nil), [Relay.Item(kind: "device", value: "80b1")])
        XCTAssertEqual(Relay.grantKey("channel", "Y2g="), "channel:Y2g=")
        XCTAssertEqual(Relay.grantURL.absoluteString, "https://matchblox.sh/push/grant")
        XCTAssertEqual(Relay.channelURL.absoluteString, "https://matchblox.sh/push/channel")
    }

    func testTheRelayRepliesDecode() throws {
        let g = try JSONDecoder().decode(Relay.GrantReply.self, from: Data(#"{"attest_grant":"k.ab","grants":{"device:80b1":"ff"}}"#.utf8))
        XCTAssertEqual(g.attestGrant, "k.ab")
        XCTAssertEqual(g.grants["device:80b1"], "ff")
        let c = try JSONDecoder().decode(Relay.ChannelReply.self, from: Data(#"{"channel":"Y2g=","grant":"ee"}"#.utf8))
        XCTAssertEqual(c, Relay.ChannelReply(channel: "Y2g=", grant: "ee"))
    }

    func testTokensAreLowercaseHex() {
        XCTAssertEqual(hexToken(Data([0x80, 0xb1, 0x0a])), "80b10a")
    }

    func testTheRequestIsOneLineGoReads() throws {
        let req = RegisterRequest(deviceToken: "80b1", grants: ["device:80b1": "ff"], notifyKey: Data([1, 2]), buzz: false)
        let line = req.line
        XCTAssertEqual(line.last, UInt8(ascii: "\n"))
        XCTAssertEqual(line.filter { $0 == UInt8(ascii: "\n") }.count, 1)
        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: line) as? [String: Any])
        XCTAssertNil(obj["island"], "a host not followed gets no island, which clears it there")
        XCTAssertEqual(obj["notify_key"] as? String, "AQI=")
        XCTAssertEqual(try JSONDecoder().decode(RegisterRequest.self, from: line), req)
    }

    func testTheReplyIsTheFirstJSONLine() async throws {
        let (stream, c) = AsyncStream<Data>.makeStream()
        c.yield(Data("Welcome to ws-1\n{\"ok\":true,\"sess".utf8))
        c.yield(Data("ions\":{\"1\":\"a1b2\"},\"accounts\":{}}\n".utf8))
        let reply = try await RegisterReply.read(from: stream)
        XCTAssertEqual(reply.sessions, [1: "a1b2"])
        c.finish()
    }

    func testARefusedOrMissingReplyThrows() async {
        let (refused, c1) = AsyncStream<Data>.makeStream()
        c1.yield(Data("{\"ok\":false,\"error\":\"unknown device\"}\n".utf8))
        c1.finish()
        do { _ = try await RegisterReply.read(from: refused); XCTFail() } catch {
            XCTAssertEqual(error, .refused("unknown device"))
        }
        let (silent, c2) = AsyncStream<Data>.makeStream()
        c2.yield(Data("motd\n".utf8))
        c2.finish()
        do { _ = try await RegisterReply.read(from: silent); XCTFail() } catch {
            XCTAssertEqual(error, .noReply)
        }
    }

    func testATappedLinkOpensThatHostsSession() throws {
        let hosts = [host("ws-1", ws1), host("work-1", work1)]
        let link = try XCTUnwrap(OpenLink(host: "a30534a5", session: 12))
        XCTAssertEqual(Opening.route(for: link, hosts: hosts), .console(hosts[1], open: 12))
        let unknown = try XCTUnwrap(OpenLink(host: "00000000", session: 1))
        XCTAssertEqual(Opening.route(for: unknown, hosts: hosts), .hosts)
        XCTAssertEqual(Opening.route(for: unknown, hosts: []), .scan)
    }

    func testTheConsoleCommandOpensTheLinkedSession() {
        XCTAssertEqual(Opening.consoleCommand(host: host("ws-1", ws1), open: nil), "matchblox console")
        XCTAssertEqual(Opening.consoleCommand(host: host("ws-1", ws1), open: 12), "matchblox console --open 12")
        XCTAssertEqual(Opening.notReachable(host: "work-1"), "work-1 is not reachable: it changes at the next connect")
    }
}
