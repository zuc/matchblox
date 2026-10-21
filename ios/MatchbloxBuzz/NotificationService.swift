import CryptoKit
import Foundation
import MatchbloxKit
import Security
import UserNotifications

/// Opens the sealed buzz (internal/push/sender.go buzzBody): the alert names nothing, and
/// `buzz.sealed` holds the line, sealed to the notify key the app keeps in the shared
/// keychain group. The opened line becomes the body, and `userInfo["open"]` the link a tap
/// opens. If anything fails, the placeholder alert goes as it came: never the ciphertext.
final class NotificationService: UNNotificationServiceExtension {
    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        guard let key = Self.notifyKey(),
              let opened = BuzzPayload.open(request.content.userInfo, open: { try openBuzz($0, key: key) }),
              let content = request.content.mutableCopy() as? UNMutableNotificationContent
        else {
            contentHandler(request.content)
            return
        }
        content.body = opened.body
        content.userInfo["open"] = opened.link.absoluteString
        contentHandler(content)
    }

    /// Everything above runs at once; nothing is left to deliver when time runs out.
    override func serviceExtensionTimeWillExpire() {}

    /// The X25519 notify key, from the group in the Info key `MatchbloxKeychainGroup`.
    static func notifyKey() -> Curve25519.KeyAgreement.PrivateKey? {
        var q: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: NotifyKeyItem.service,
            kSecAttrAccount: NotifyKeyItem.account,
            kSecReturnData: true,
        ]
        if let group = Bundle.main.object(forInfoDictionaryKey: "MatchbloxKeychainGroup") as? String, !group.isEmpty {
            q[kSecAttrAccessGroup] = group
        }
        var item: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &item) == errSecSuccess, let raw = item as? Data else { return nil }
        return try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: raw)
    }
}
