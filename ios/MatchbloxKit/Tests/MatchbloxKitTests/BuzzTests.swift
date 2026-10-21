import CryptoKit
import XCTest
@testable import MatchbloxKit

final class BuzzTests: XCTestCase {
    struct Vector: Decodable {
        let privateKey: Data
        let plain: String
        let sealed: Data
        enum CodingKeys: String, CodingKey { case privateKey = "private_key", plain, sealed }
    }

    func testTheGoVectorOpens() throws {
        let v: Vector = try PushFixture.load("seal-vector")
        let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: v.privateKey)
        XCTAssertEqual(try openBuzz(v.sealed, key: key), v.plain)
    }

    func testATamperedBuzzIsRefused() throws {
        let v: Vector = try PushFixture.load("seal-vector")
        let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: v.privateKey)
        var bad = v.sealed
        bad[bad.count - 1] ^= 1
        XCTAssertThrowsError(try openBuzz(bad, key: key))
        XCTAssertThrowsError(try openBuzz(Data(repeating: 0, count: 10), key: key))
    }
}
