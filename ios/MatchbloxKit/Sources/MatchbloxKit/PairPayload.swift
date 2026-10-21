import Foundation

public enum PairError: Error, Equatable, Sendable {
    case notMatchblox
    case version(Int)
    case malformed
}

public struct PairPayload: Codable, Equatable, Sendable {
    public var v: Int
    public var host: String
    public var port: Int
    public var user: String
    public var name: String
    public var hostkey: String
    public var key: String

    public init(url: URL) throws(PairError) {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              components.scheme?.lowercased() == "matchblox",
              components.host?.lowercased() == "pair"
        else { throw .notMatchblox }

        guard let encoded = components.queryItems?.first(where: { $0.name == "p" })?.value,
              !encoded.isEmpty,
              let bytes = Self.decodeBase64URL(encoded),
              let first = bytes.first
        else { throw .malformed }

        let raw: Raw
        switch first {
        case UInt8(ascii: "{"):
            guard let json = try? JSONDecoder().decode(Raw.self, from: bytes) else { throw .malformed }
            guard json.v == 1 else { throw .version(json.v ?? 0) }
            raw = json
        case 2:
            raw = try Self.rawV2(bytes)
        case ..<0x20:
            throw .version(Int(first))
        default:
            throw .malformed
        }

        let version = raw.v ?? 0

        guard let host = raw.host, !host.isEmpty,
              let user = raw.user, !user.isEmpty,
              let name = raw.name, !name.isEmpty,
              let hostkey = raw.hostkey, !hostkey.isEmpty,
              let key = raw.key, !key.isEmpty,
              let port = raw.port, (1...65535).contains(port),
              Data(base64Encoded: key)?.count == 32
        else { throw .malformed }

        self.init(v: version, host: host, port: port, user: user, name: name, hostkey: hostkey, key: key)
    }

    public init(v: Int, host: String, port: Int, user: String, name: String, hostkey: String, key: String) {
        self.v = v
        self.host = host
        self.port = port
        self.user = user
        self.name = name
        self.hostkey = hostkey
        self.key = key
    }

    private struct Raw: Decodable {
        let v: Int?
        let host: String?
        let port: Int?
        let user: String?
        let name: String?
        let hostkey: String?
        let key: String?
    }

    private static let v2Head = 3 + 32 + 32

    /// The v2 bytes: 2, port (2 bytes), sha256 of the host key, the seed, then host 0 user 0 name.
    private static func rawV2(_ bytes: Data) throws(PairError) -> Raw {
        let bytes = Array(bytes)
        guard bytes.count >= v2Head else { throw .malformed }
        let parts = bytes[v2Head...].split(separator: 0, omittingEmptySubsequences: false)
        guard parts.count == 3,
              let host = String(data: Data(parts[0]), encoding: .utf8),
              let user = String(data: Data(parts[1]), encoding: .utf8),
              let name = String(data: Data(parts[2]), encoding: .utf8)
        else { throw .malformed }
        let fingerprint = Data(bytes[3..<35]).base64EncodedString().replacingOccurrences(of: "=", with: "")
        return Raw(
            v: 2, host: host, port: Int(bytes[1]) << 8 | Int(bytes[2]), user: user, name: name,
            hostkey: "SHA256:" + fingerprint, key: Data(bytes[35..<v2Head]).base64EncodedString()
        )
    }

    private static func decodeBase64URL(_ text: String) -> Data? {
        guard !text.contains("="), text.count % 4 != 1 else { return nil }
        let standard = text
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        return Data(base64Encoded: standard + String(repeating: "=", count: (4 - standard.count % 4) % 4))
    }
}
