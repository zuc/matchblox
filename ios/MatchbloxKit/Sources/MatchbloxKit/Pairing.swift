import Foundation

public struct PairDraft: Equatable, Sendable {
    public let payload: PairPayload
    public var address: String
    public var port: Int
    public var phoneName: String

    public init(payload: PairPayload, phoneName: String = "iPhone") {
        self.payload = payload
        address = payload.host
        port = payload.port
        self.phoneName = phoneName
    }
}

public enum PairRefusal: String, CaseIterable, Equatable, Sendable {
    case badName = "bad name"
    case badKey = "bad key"
    case codeUsed = "code used"
    case keyInUse = "key in use"
    case writeFailed = "write failed"
    case tooSlow = "too slow"
}

public enum PairReply: Equatable, Sendable {
    case ok(host: String)
    case failed(PairRefusal)
}

public enum PairReplyError: Error, CaseIterable, Equatable, Sendable {
    /// No line of the output was a JSON object.
    case noReply
    /// A JSON object that this app does not know, such as `bad request`.
    case unknownAnswer
}

public enum Pairing {
    private static let nameMarks: Set<Unicode.Scalar> = [" ", ".", "_", "-", "'", "’"]
    private static let maxNameLength = 40
    private static let maxReplyBytes = 64 * 1024

    /// The rules of `setup.ValidDeviceName` on the host.
    public static func isValidName(_ name: String) -> Bool {
        let scalars = name.unicodeScalars
        return (1...maxNameLength).contains(scalars.count)
            && scalars.first != " "
            && scalars.last != " "
            && scalars.allSatisfy(isNameScalar)
    }

    /// The first `matchblox://pair?p=…` token of pasted text. The token ends at the first character that base64url does not use.
    public static func firstLink(in text: String) -> URL? {
        guard let start = text.range(of: "matchblox://pair?p=", options: .caseInsensitive) else { return nil }
        let payload = text[start.upperBound...].prefix { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") }
        return URL(string: String(text[start.lowerBound..<start.upperBound]) + payload)
    }

    public static func cleanName(_ name: String) -> String {
        var scalars = String.UnicodeScalarView(name.unicodeScalars.filter(isNameScalar))
        scalars = String.UnicodeScalarView(scalars.drop { $0 == " " }.prefix(maxNameLength))
        while scalars.last == " " {
            scalars.removeLast()
        }
        return scalars.isEmpty ? "iPhone" : String(scalars)
    }

    public static func requestLine(name: String, key: String) -> Data {
        Data("{\"name\":\(quoted(name)),\"key\":\(quoted(key))}\n".utf8)
    }

    /// The first line of `output` that is a JSON object is the reply. The login shell of the host can print other lines first.
    public static func reply(_ output: Data) throws(PairReplyError) -> PairReply {
        guard let answer = firstAnswer(in: output, completeLinesOnly: false) else { throw .noReply }
        return try decode(answer)
    }

    /// Returns at the first complete JSON line, with no wait for the end of the stream.
    public static func readReply(from stream: AsyncStream<Data>) async throws(PairReplyError) -> PairReply {
        var output = Data()
        for await chunk in stream {
            output.append(chunk)
            if let answer = firstAnswer(in: output, completeLinesOnly: true) {
                return try decode(answer)
            }
            if output.count > maxReplyBytes { break }
        }
        return try reply(output)
    }

    /// The host key comes fresh from the code, so a host key that does not match means the address reaches another machine.
    public static func message(for failure: ConnectFailure, host: String, address: String) -> String {
        switch failure {
        case .refused:
            codeUsedMessage(host: host)
        case .unreachable:
            "Can't reach \(host). Check the address, or that this device is on your network."
        case .hostKeyChanged:
            "\(address) is not \(host). Check the address, then tap Try again."
        case .closed:
            "The connection to \(host) closed before pairing was done. Run matchblox pair on \(host) again."
        }
    }

    public static func message(for refusal: PairRefusal, host: String) -> String {
        switch refusal {
        case .badName:
            "\(host) refused the name of this device. Scan the code again."
        case .badKey:
            "\(host) refused the key of this device. Update matchblox on \(host), then run matchblox pair again."
        case .codeUsed:
            codeUsedMessage(host: host)
        case .keyInUse:
            "The key of this device already opens a login on \(host). Remove its line from ~/.ssh/authorized_keys on \(host), then run matchblox pair again."
        case .writeFailed:
            "\(host) could not save the key of this device. Check that ~/.ssh on \(host) is writable, then scan the code again."
        case .tooSlow:
            "\(host) did not get the key of this device in time. Scan the code again."
        }
    }

    public static func message(for error: PairReplyError, host: String) -> String {
        switch error {
        case .noReply:
            "\(host) did not answer. Check the network, or update matchblox on \(host), then run matchblox pair again."
        case .unknownAnswer:
            "\(host) gave an answer that this app does not know. Update matchblox on \(host) and update this app, then try again."
        }
    }

    public static func message(for error: PairError) -> String {
        switch error {
        case .notMatchblox:
            "This is not a matchblox pairing code. Run matchblox pair on your host, then scan its code."
        case .version:
            "This code is for a different version of matchblox. Update matchblox on your host and update this app, then scan again."
        case .malformed:
            "This code is damaged. Run matchblox pair on your host again, then scan the new code."
        }
    }

    private static func codeUsedMessage(host: String) -> String {
        "This code was used or expired. Run matchblox pair on \(host) again."
    }

    private static func isNameScalar(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter, .decimalNumber:
            true
        default:
            nameMarks.contains(scalar)
        }
    }

    private struct Answer: Decodable {
        let ok: Bool?
        let host: String?
        let error: String?
    }

    private static func firstAnswer(in output: Data, completeLinesOnly: Bool) -> Answer? {
        var lines = output.split(separator: UInt8(ascii: "\n"), omittingEmptySubsequences: false)
        if completeLinesOnly {
            lines.removeLast()
        }
        for line in lines {
            let text = String(decoding: line, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
            guard text.hasPrefix("{") else { continue }
            if let answer = try? JSONDecoder().decode(Answer.self, from: Data(text.utf8)) {
                return answer
            }
        }
        return nil
    }

    private static func decode(_ answer: Answer) throws(PairReplyError) -> PairReply {
        if answer.ok == true, let host = answer.host, !host.isEmpty {
            return .ok(host: host)
        }
        if answer.ok == false, let word = answer.error, let refusal = PairRefusal(rawValue: word) {
            return .failed(refusal)
        }
        throw .unknownAnswer
    }

    private static func quoted(_ text: String) -> String {
        var out = "\""
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case _ where scalar.value < 0x20:
                out += String(format: "\\u%04x", scalar.value)
            default:
                out.unicodeScalars.append(scalar)
            }
        }
        return out + "\""
    }
}
