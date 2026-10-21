import CryptoKit
import Foundation

public enum BuzzError: Error { case malformed, notText }

/// Opens a sealed buzz: HPKE base mode, Curve25519 / SHA-256 / ChaCha20-Poly1305, info
/// `matchblox buzz v1`, empty AAD. `sealed` is enc (32 bytes) followed by the ciphertext.
public func openBuzz(_ sealed: Data, key: Curve25519.KeyAgreement.PrivateKey) throws -> String {
    let encLen = 32
    guard sealed.count > encLen else { throw BuzzError.malformed }
    let bytes = Data(sealed)  // rebase indices
    let enc = bytes.prefix(encLen)
    let ciphertext = bytes.dropFirst(encLen)
    var recipient = try HPKE.Recipient(
        privateKey: key,
        ciphersuite: HPKE.Ciphersuite(kem: .Curve25519_HKDF_SHA256, kdf: .HKDF_SHA256, aead: .chaChaPoly),
        info: Data("matchblox buzz v1".utf8),
        encapsulatedKey: enc)
    let plain = try recipient.open(ciphertext, authenticating: Data())
    guard let text = String(data: plain, encoding: .utf8) else { throw BuzzError.notText }
    return text
}
