import XCTest
@testable import MatchbloxKit

final class OpenLinkTests: XCTestCase {
    func testTheOpenLinkRoundTrips() throws {
        let link = try XCTUnwrap(OpenLink(host: "f10f7812", session: 12))
        XCTAssertEqual(link.url.absoluteString, "matchblox://open?host=f10f7812&session=12")
        let back = try XCTUnwrap(OpenLink(url: link.url))
        XCTAssertEqual(back, link)
        XCTAssertEqual(back.command, "matchblox console --open 12")
    }

    func testAnOpenLinkWithABadNumberIsRefused() {
        for s in [
            "matchblox://open?host=f10f7812&session=0",
            "matchblox://open?host=f10f7812&session=-1",
            "matchblox://open?host=f10f7812&session=x",
            "matchblox://open?host=f10f7812",
            "matchblox://open?host=F10F7812&session=1",
            "matchblox://open?host=f10f781&session=1",
            "matchblox://open?host=f10f78123&session=1",
            "matchblox://open?host=g10f7812&session=1",
            "matchblox://close?host=f10f7812&session=1",
            "https://open?host=f10f7812&session=1",
        ] {
            XCTAssertNil(OpenLink(url: URL(string: s)!), s)
        }
        XCTAssertNil(OpenLink(host: "f10f7812", session: 0))
        XCTAssertNil(OpenLink(host: "zz", session: 1))
    }
}
