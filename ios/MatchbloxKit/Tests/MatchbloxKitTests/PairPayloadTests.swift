import Foundation
import XCTest
@testable import MatchbloxKit

final class PairPayloadTests: XCTestCase {
    private func url(of payload: PairPayload) throws -> URL {
        try url(carrying: JSONEncoder().encode(payload))
    }

    private func url(carrying json: Data) -> URL {
        URL(string: "matchblox://pair?p=" + base64URL(json))!
    }

    private func base64URL(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    func testFixtureURL() throws {
        let fixture = try Fixture.load()
        let parsed = try PairPayload(url: URL(string: fixture.url)!)
        XCTAssertEqual(parsed, fixture.payload)
    }

    func testIgnoresUnknownFields() throws {
        let fixture = try Fixture.load()
        var object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(fixture.payload)) as! [String: Any]
        object["extra"] = true
        let json = try JSONSerialization.data(withJSONObject: object)
        XCTAssertEqual(try PairPayload(url: url(carrying: json)), fixture.payload)
    }

    func testRefuses() throws {
        let fixture = try Fixture.load()
        func with(_ change: (inout PairPayload) -> Void) throws -> URL {
            var payload = fixture.payload
            change(&payload)
            return try url(of: payload)
        }
        let cases: [(String, URL, PairError)] = [
            ("v 2", try with { $0.v = 2 }, .version(2)),
            ("no host", try with { $0.host = "" }, .malformed),
            ("no user", try with { $0.user = "" }, .malformed),
            ("no name", try with { $0.name = "" }, .malformed),
            ("no hostkey", try with { $0.hostkey = "" }, .malformed),
            ("port 0", try with { $0.port = 0 }, .malformed),
            ("port 65536", try with { $0.port = 65536 }, .malformed),
            ("31-byte key", try with { $0.key = Data(count: 31).base64EncodedString() }, .malformed),
            ("not base64", URL(string: "matchblox://pair?p=!!!")!, .malformed),
            ("padded base64", URL(string: fixture.url + "%3D")!, .malformed),
            ("not json", url(carrying: Data("x".utf8)), .malformed),
            ("no p", URL(string: "matchblox://pair")!, .malformed),
            ("scheme", URL(string: fixture.url.replacingOccurrences(of: "matchblox://", with: "https://"))!, .notMatchblox),
            ("host", URL(string: fixture.url.replacingOccurrences(of: "://pair", with: "://other"))!, .notMatchblox),
        ]
        for (name, url, expected) in cases {
            XCTAssertThrowsError(try PairPayload(url: url), name) { error in
                XCTAssertEqual(error as? PairError, expected, name)
            }
        }
    }

    func testV2FixtureParses() throws {
        let v1 = try Fixture.load().payload
        let v2 = try FixtureV2.load()
        var expected = v1
        expected.v = 2
        XCTAssertEqual(v2.payload, expected)
        XCTAssertEqual(try PairPayload(url: URL(string: v2.url)!), v2.payload)
    }

    /// firstLink finds the link in any case, so the payload reads it in any case too.
    func testSchemeAndHostInAnyCase() throws {
        let v2 = try FixtureV2.load()
        let token = v2.url.dropFirst("matchblox://pair?p=".count)
        for head in ["MATCHBLOX://PAIR?p=", "Matchblox://Pair?p="] {
            let pasted = try XCTUnwrap(Pairing.firstLink(in: "Open \(head)\(token) now"), head)
            XCTAssertEqual(try PairPayload(url: pasted), v2.payload, head)
        }
    }

    func testV2Refusals() throws {
        let v2 = try FixtureV2.load()
        let good = Data(base64URL: String(v2.url.dropFirst("matchblox://pair?p=".count)))!
        let head = 67
        func link(_ bytes: Data) -> URL { url(carrying: bytes) }
        func with(_ tail: String) -> URL { link(good.prefix(head) + Data(tail.utf8)) }
        let p = v2.payload
        func port(_ n: Int) -> URL {
            var bytes = good
            bytes[1] = UInt8(n >> 8)
            bytes[2] = UInt8(n & 0xff)
            return link(bytes)
        }
        let cases: [(String, URL, PairError)] = [
            ("short", link(good.prefix(40)), .malformed),
            ("version 3", link(Data([3]) + good.dropFirst()), .version(3)),
            ("no separator", with("\(p.host) \(p.user)\0\(p.name)"), .malformed),
            ("one separator", with("\(p.host)\0\(p.user)\(p.name)"), .malformed),
            ("four parts", with("\(p.host)\0\(p.user)\0\(p.name)\0x"), .malformed),
            ("no host", with("\0\(p.user)\0\(p.name)"), .malformed),
            ("no user", with("\(p.host)\0\0\(p.name)"), .malformed),
            ("no name", with("\(p.host)\0\(p.user)\0"), .malformed),
            ("port 0", port(0), .malformed),
            ("bad base64", URL(string: "matchblox://pair?p=!!!")!, .malformed),
        ]
        for (name, url, expected) in cases {
            XCTAssertThrowsError(try PairPayload(url: url), name) { error in
                XCTAssertEqual(error as? PairError, expected, name)
            }
        }
    }
}

private extension Data {
    init?(base64URL text: String) {
        let standard = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        self.init(base64Encoded: standard + String(repeating: "=", count: (4 - standard.count % 4) % 4))
    }
}
