import CryptoKit
import Foundation
import LocalAuthentication
import MatchbloxKit
import NIOSSH
import Security

enum DeviceKeyError: Error, Equatable {
    case noPasscode
    case keyUnavailable
    case locked
    case keychain(OSStatus)
}

extension DeviceKeyError: LocalizedError {
    var errorDescription: String? {
        switch self {
        case .noPasscode:
            "This device has no passcode. Set a passcode in Settings, then try again."
        case .keyUnavailable:
            "This device can't open its key. Run matchblox pair on your host, then scan the code again."
        case .locked:
            "This device is locked. Unlock it, then try again."
        case .keychain(let status):
            "The Keychain refused the key of this device (error \(status)). Restart this device, then try again."
        }
    }
}

/// One Secure Enclave key for each paired host. The Keychain item of a host has its host key fingerprint as the account.
enum DeviceKey {
    private static let service = "com.blox.matchblox.device"

    /// The public key that pairing sends to the host. It makes the key of the host when there is none.
    static func openSSH(for host: String) throws -> String {
        let key = try loadOrCreate(host: host)
        return SSHKeys.openSSH(p256X963: key.publicKey.x963Representation, comment: "")
    }

    static func load(host: String, context: LAContext) throws -> SecureEnclave.P256.Signing.PrivateKey {
        guard let stored = try storedRepresentation(host: host) else { throw DeviceKeyError.keyUnavailable }
        return try open(stored, context: context)
    }

    static func remove(host: String) throws {
        let status = SecItemDelete(identity(host: host) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw keychainError(status) }
    }

    static func nioKey(_ key: SecureEnclave.P256.Signing.PrivateKey) -> NIOSSHPrivateKey {
        NIOSSHPrivateKey(secureEnclaveP256Key: key)
    }

    /// A stored key that no longer opens belongs to an old pairing of this host, so a new key replaces it. A key that cannot be made is not retried.
    private static func loadOrCreate(host: String) throws -> SecureEnclave.P256.Signing.PrivateKey {
        guard let stored = try storedRepresentation(host: host) else { return try create(host: host) }
        do {
            return try open(stored, context: nil)
        } catch DeviceKeyError.keyUnavailable {
            try remove(host: host)
            return try create(host: host)
        }
    }

    private static func create(host: String) throws -> SecureEnclave.P256.Signing.PrivateKey {
        var probeError: NSError?
        guard LAContext().canEvaluatePolicy(.deviceOwnerAuthentication, error: &probeError) else {
            throw DeviceKeyError.noPasscode
        }
        var error: Unmanaged<CFError>?
        guard let access = SecAccessControlCreateWithFlags(
            nil,
            kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
            [.privateKeyUsage, .biometryAny, .or, .devicePasscode],
            &error
        ) else { throw DeviceKeyError.keyUnavailable }
        let key: SecureEnclave.P256.Signing.PrivateKey
        do {
            key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
        } catch {
            throw DeviceKeyError.keyUnavailable
        }
        let status = add(key.dataRepresentation, host: host)
        if status == errSecDuplicateItem, let stored = try storedRepresentation(host: host) {
            return try open(stored, context: nil)
        }
        guard status == errSecSuccess else { throw keychainError(status) }
        return key
    }

    private static func open(
        _ representation: Data,
        context: LAContext?
    ) throws -> SecureEnclave.P256.Signing.PrivateKey {
        do {
            return try SecureEnclave.P256.Signing.PrivateKey(
                dataRepresentation: representation,
                authenticationContext: context
            )
        } catch {
            throw DeviceKeyError.keyUnavailable
        }
    }

    private static func keychainError(_ status: OSStatus) -> DeviceKeyError {
        status == errSecInteractionNotAllowed ? .locked : .keychain(status)
    }

    private static func identity(host: String) -> [CFString: Any] {
        [kSecClass: kSecClassGenericPassword, kSecAttrService: service, kSecAttrAccount: host]
    }

    private static func storedRepresentation(host: String) throws -> Data? {
        var query = identity(host: host)
        query[kSecReturnData] = true
        var item: CFTypeRef?
        switch SecItemCopyMatching(query as CFDictionary, &item) {
        case errSecSuccess: return item as? Data
        case errSecItemNotFound: return nil
        case let status: throw keychainError(status)
        }
    }

    private static func add(_ representation: Data, host: String) -> OSStatus {
        var item = identity(host: host)
        item[kSecValueData] = representation
        item[kSecAttrAccessible] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        return SecItemAdd(item as CFDictionary, nil)
    }
}
