import UIKit

/// The haptics of the pair screens (design/0009 §6.2, §6.5). They play also
/// with Reduce Motion. One generator of each kind stays, so a prepared one
/// plays on time.
@MainActor
enum Haptics {
    private static let light = UIImpactFeedbackGenerator(style: .light)
    private static let rigid = UIImpactFeedbackGenerator(style: .rigid)
    private static let notice = UINotificationFeedbackGenerator()

    /// Wakes the generators. Call it when pairing starts.
    static func prepare() {
        light.prepare()
        rigid.prepare()
        notice.prepare()
    }

    /// The scanner found a pair code.
    static func lock() {
        light.impactOccurred()
    }

    /// The match head reaches the end of the striker.
    static func strike() {
        rigid.impactOccurred()
    }

    /// The match stands and burns: the device is paired.
    static func stood() {
        notice.notificationOccurred(.success)
    }

    /// The pairing failed.
    static func failed() {
        notice.notificationOccurred(.error)
    }
}
