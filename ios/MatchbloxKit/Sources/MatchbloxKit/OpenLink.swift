import Foundation

/// `matchblox://open?host=<8 hex>&session=<n>`: opens a session of a host in the app.
public struct OpenLink: Equatable, Sendable {
    public let host: String
    public let session: Int

    public init?(host: String, session: Int) {
        guard OpenLink.isHost8(host), session > 0 else { return nil }
        self.host = host
        self.session = session
    }

    public init?(url: URL) {
        guard url.scheme == "matchblox", url.host == "open", url.path.isEmpty,
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              items.count == 2,
              let host = items.first(where: { $0.name == "host" })?.value,
              let s = items.first(where: { $0.name == "session" })?.value,
              s.first != "0", s.allSatisfy({ $0.isASCII && $0.isNumber }), let n = Int(s)
        else { return nil }
        self.init(host: host, session: n)
    }

    public var url: URL {
        URL(string: "matchblox://open?host=\(host)&session=\(session)")!
    }

    /// The command that opens the same session on the host.
    public var command: String { "matchblox console --open \(session)" }

    static func isHost8(_ s: String) -> Bool {
        s.utf8.count == 8 && s.utf8.allSatisfy { ($0 >= 48 && $0 <= 57) || ($0 >= 97 && $0 <= 102) }
    }
}
