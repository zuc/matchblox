import CryptoKit
import DeviceCheck
import Foundation
import MatchbloxKit
import Security

/// The relay's grants and channels, asked for with App Attest (worker/push.js). The first
/// call attests the key; later calls assert with it. The key id and the attest grant stay in
/// this app's keychain.
@MainActor
enum Attest {
    enum Failure: Error, CustomStringConvertible {
        case unsupported
        case relay(status: Int, error: String?)
        case badReply

        var description: String {
            switch self {
            case .unsupported: "App Attest is not available on this device"
            case .relay(let status, let error): "relay \(status): \(error ?? "no reason")"
            case .badReply: "the relay's reply is not what this app reads"
            }
        }
    }

    private static let service = "com.blox.matchblox.attest"
    /// The app's own access group, never the one the extensions share.
    private static let group = Bundle.main.object(forInfoDictionaryKey: "MatchbloxOwnKeychainGroup") as? String
    private static let keyIdAccount = "key-id"
    private static let grantAccount = "attest-grant"

    /// One grant per item, keyed `<kind>:<value>`.
    static func grants(items: [Relay.Item]) async throws -> [String: String] {
        let clientData = Relay.clientData(items: items, time: Int(Date().timeIntervalSince1970))
        let data = try await post(Relay.grantURL, clientData: clientData)
        guard let reply = try? JSONDecoder().decode(Relay.GrantReply.self, from: data) else { throw Failure.badReply }
        Keychain.set(Data(reply.attestGrant.utf8), service: service, account: grantAccount, group: group)
        return reply.grants
    }

    /// A new broadcast channel and its grant.
    static func channel() async throws -> ChannelGrant {
        // The channel call takes an assertion; the attest grant comes from a grant call.
        if Keychain.get(service: service, account: grantAccount, group: group) == nil {
            _ = try await grants(items: [])
        }
        let clientData = Relay.clientData(items: nil, time: Int(Date().timeIntervalSince1970))
        let data = try await post(Relay.channelURL, clientData: clientData)
        guard let reply = try? JSONDecoder().decode(Relay.ChannelReply.self, from: data) else { throw Failure.badReply }
        return ChannelGrant(channel: reply.channel, grant: reply.grant)
    }

    private static func post(_ url: URL, clientData: String) async throws -> Data {
        let attest = DCAppAttestService.shared
        guard attest.isSupported else { throw Failure.unsupported }
        let hash = Data(SHA256.hash(data: Data(clientData.utf8)))
        var body: [String: String] = ["client_data": clientData]

        let stored = Keychain.get(service: service, account: keyIdAccount, group: group).map { String(decoding: $0, as: UTF8.self) }
        let attestGrant = Keychain.get(service: service, account: grantAccount, group: group).map { String(decoding: $0, as: UTF8.self) }
        if let keyId = stored, let attestGrant {
            body["key_id"] = keyId
            body["attest_grant"] = attestGrant
            body["assertion"] = try await attest.generateAssertion(keyId, clientDataHash: hash).base64EncodedString()
        } else {
            // A key is attested once: an old key without its grant gives way to a new one.
            let keyId = try await attest.generateKey()
            Keychain.set(Data(keyId.utf8), service: service, account: keyIdAccount, group: group)
            Keychain.remove(service: service, account: grantAccount, group: group)
            body["key_id"] = keyId
            body["attestation"] = try await attest.attestKey(keyId, clientDataHash: hash).base64EncodedString()
        }

        var request = URLRequest(url: url, timeoutInterval: 15)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else {
            let reason = (try? JSONDecoder().decode(Relay.Refusal.self, from: data))?.error
            if status == 403, let reason, reason.hasPrefix("app attest") || reason == "bad attest grant"
                || reason == "key_id is not the attested key" {
                // A grant or key the relay no longer takes: attest a new key next time.
                Keychain.remove(service: service, account: grantAccount, group: group)
            }
            throw Failure.relay(status: status, error: reason)
        }
        return data
    }
}

/// Small generic-password items in the given access group. With no group, an item goes to the
/// first group of the app's entitlements, which is the app's own.
enum Keychain {
    private static func identity(service: String, account: String, group: String?) -> [CFString: Any] {
        var q: [CFString: Any] = [kSecClass: kSecClassGenericPassword, kSecAttrService: service, kSecAttrAccount: account]
        if let group, !group.isEmpty { q[kSecAttrAccessGroup] = group }
        return q
    }

    static func get(service: String, account: String, group: String? = nil) -> Data? {
        var q = identity(service: service, account: account, group: group)
        q[kSecReturnData] = true
        var item: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &item) == errSecSuccess else { return nil }
        return item as? Data
    }

    @discardableResult
    static func set(_ data: Data, service: String, account: String, group: String? = nil) -> Bool {
        let q = identity(service: service, account: account, group: group)
        let update = SecItemUpdate(q as CFDictionary, [kSecValueData: data] as CFDictionary)
        if update == errSecSuccess { return true }
        var item = q
        item[kSecValueData] = data
        item[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        return SecItemAdd(item as CFDictionary, nil) == errSecSuccess
    }

    static func remove(service: String, account: String, group: String? = nil) {
        SecItemDelete(identity(service: service, account: account, group: group) as CFDictionary)
    }
}
