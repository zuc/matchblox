import XCTest
@testable import MatchbloxKit
import struct MatchbloxKit.Host

final class OpeningTests: XCTestCase {
    private func host(_ label: String) -> Host {
        Host(label: label, address: "\(label).local", port: 22, user: "dev", fingerprint: "SHA256:\(label)")
    }

    func testFirstRoute() {
        XCTAssertEqual(Opening.first(hosts: []), .scan)
        XCTAssertEqual(Opening.first(hosts: [host("ws-1")]), .console(host("ws-1"), open: nil))
        XCTAssertEqual(Opening.first(hosts: [host("ws-1"), host("ws-2")]), .hosts)
    }

    func testReconnectMessages() {
        XCTAssertEqual(Opening.message(for: .unreachable, host: "ws-1"), "Can't reach ws-1.")
        XCTAssertEqual(
            Opening.message(for: .hostKeyChanged, host: "ws-1"),
            "ws-1 has a new host key. Run matchblox pair on ws-1 again."
        )
        XCTAssertEqual(
            Opening.message(for: .refused, host: "ws-1"),
            "The key of this device no longer opens ws-1. Run matchblox pair on ws-1, then scan the new code."
        )
        XCTAssertEqual(
            Opening.message(for: .closed("x"), host: "ws-1"),
            "The connection to ws-1 closed. Tap Try again, or scan again."
        )
    }

    func testReconnectMessagesAreDistinct() {
        let failures: [ConnectFailure] = [.unreachable, .hostKeyChanged, .refused, .closed("x")]
        let messages = failures.map { Opening.message(for: $0, host: "ws-1") }
            + [Opening.removedNotice("ws-1"), Opening.endedMessage(host: "ws-1"), Opening.stopsAtOnceMessage(host: "ws-1")]
        XCTAssertEqual(Set(messages).count, messages.count)
        for message in messages {
            XCTAssertFalse(message.replacingOccurrences(of: "iPhone", with: "").lowercased().contains("phone"), message)
        }
    }

    func testRemovedNoticeNamesTheFix() {
        XCTAssertEqual(
            Opening.removedNotice("ws-1"),
            "ws-1 is removed. Remove its key on the host with matchblox devices rm."
        )
    }

    func testClosedMessageHidesTheDebugText() {
        let debug = "NWError: POSIX 54 secret-detail"
        let message = Opening.message(for: .closed(debug), host: "ws-1")
        XCTAssertFalse(message.contains(debug))
        XCTAssertTrue(message.hasSuffix("."))
        XCTAssertNotEqual(message, Pairing.message(for: .closed(debug), host: "ws-1", address: "ws-1.local"))
    }

    func testReconnectRefusedIsNotCodeUsed() {
        XCTAssertNotEqual(
            Opening.message(for: .refused, host: "ws-1"),
            Pairing.message(for: .refused, host: "ws-1", address: "ws-1.local")
        )
    }
}
