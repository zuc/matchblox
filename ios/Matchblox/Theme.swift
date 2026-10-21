import SwiftUI

/// The dark console tokens of DESIGN.md §2.1, read from `Tokens`.
enum Theme {
    static let bg = Tokens.bg
    static let fg = Tokens.fg
    static let muted = Tokens.muted
    static let faint = Tokens.faint
    static let hair = Tokens.hair
    static let accent = Tokens.accent
    static let neg = Tokens.neg

    // The match and the mark.
    static let flame = Tokens.flame
    static let flameAmber = Tokens.flameAmber
    static let flameCore = Tokens.flameCore
    static let matchHead = Tokens.matchHead
    static let markTop = Tokens.markTop
    static let markLeft = Tokens.markLeft
    static let markRight = Tokens.markRight
    static let markTray = Tokens.markTray
    static let markStriker = Tokens.markStriker
    static let markStick = Tokens.markStick
}
