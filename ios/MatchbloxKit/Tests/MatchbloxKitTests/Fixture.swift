import Foundation
@testable import MatchbloxKit

struct Fixture: Codable {
    struct Names: Codable {
        let good: [String]
        let bad: [String]
    }

    struct Request: Codable {
        let name: String
        let key: String
    }

    let names: Names
    let hostkeyPub: String
    let hostkeyFp: String
    let payload: PairPayload
    let url: String
    let request: Request
    let requestLine: String
    let replies: [String: String]
    let p256Openssh: String
    let p256X963Hex: String

    enum CodingKeys: String, CodingKey {
        case names, payload, url, request, replies
        case hostkeyPub = "hostkey_pub"
        case hostkeyFp = "hostkey_fp"
        case requestLine = "request_line"
        case p256Openssh = "p256_openssh"
        case p256X963Hex = "p256_x963_hex"
    }

    static func load() throws -> Fixture {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let file = root.appendingPathComponent("testdata/pair/v1.json")
        return try JSONDecoder().decode(Fixture.self, from: Data(contentsOf: file))
    }
}

/// Loader for the shared testdata/push fixtures, which Go tests read too.
enum PushFixture {
    static func data(_ name: String) throws -> Data {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        return try Data(contentsOf: root.appendingPathComponent("testdata/push/\(name).json"))
    }

    static func load<T: Decodable>(_ name: String, as type: T.Type = T.self) throws -> T {
        try IslandJSON.decoder.decode(T.self, from: data(name))
    }
}
