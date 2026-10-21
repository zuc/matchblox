import XCTest
@testable import MatchbloxKit

final class IslandNamesTests: XCTestCase {
    func testAnUnknownNumberIsNamedByItsNumber() {
        let n = IslandNames(sessions: [3: "api"], accounts: [1: "work"])
        XCTAssertEqual(n.session(3), "api")
        XCTAssertEqual(n.session(12), "session 12")
        XCTAssertEqual(n.account(1), "work")
        XCTAssertEqual(n.account(3), "account 3")
    }

    func testNamesRoundTripAndLiveInTheGroupUnderTheHost() throws {
        let n = IslandNames(sessions: [3: "api", 12: "web"], accounts: [1: "work"])
        let back = try JSONDecoder().decode(IslandNames.self, from: JSONEncoder().encode(n))
        XCTAssertEqual(back, n)
        XCTAssertEqual(IslandNames.fileName(host: "f10f7812"), "island-names-f10f7812.json")
    }
}
