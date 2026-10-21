import UserNotifications

/// A stub: the buzz passes the notification through as it came.
final class NotificationService: UNNotificationServiceExtension {
    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        contentHandler(request.content)
    }

    override func serviceExtensionTimeWillExpire() {}
}
