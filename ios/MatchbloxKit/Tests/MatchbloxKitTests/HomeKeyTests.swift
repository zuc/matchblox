import XCTest
@testable import MatchbloxKit

final class HomeKeyTests: XCTestCase {
    private struct KeyFixture: Decodable {
        let sequence: String
        let label: String
    }

    private func cells(_ row: String) -> [Character] { Array(row) }

    func testMatchesTheHost() throws {
        let fixture = try JSONDecoder().decode(KeyFixture.self, from: testdata("home/key.json"))
        XCTAssertEqual(HomeKey.bytes, Array(fixture.sequence.utf8))
        XCTAssertEqual(HomeKey.label, fixture.label)
    }

    func testFindsTheLabel() {
        XCTAssertEqual(HomeKey.labelColumns(in: cells("[api] 0:zsh  ◂ matchblox  12:40")), 13..<24)
        XCTAssertEqual(HomeKey.labelColumns(in: cells("◂ matchblox")), 0..<11)
    }

    func testNoLabel() {
        XCTAssertNil(HomeKey.labelColumns(in: cells("[api] 0:zsh*  12:40")))
        XCTAssertNil(HomeKey.labelColumns(in: cells("◂ matchbl")))
        XCTAssertNil(HomeKey.labelColumns(in: []))
    }
}
