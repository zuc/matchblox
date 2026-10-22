import MatchbloxKit
import SwiftTerm
import UIKit

/// The console's terminal, with three ways back to matchblox: a swipe in from the left edge,
/// a tap on "◂ matchblox" in the tmux status line, and ⌘M. Each one sends `HomeKey`.
final class ConsoleTerminalView: TerminalView {
    var onHome: @MainActor () -> Void = {}

    private let haptic = UIImpactFeedbackGenerator(style: .light)
    private var edge: UIScreenEdgePanGestureRecognizer?
    private var labelTap: UITapGestureRecognizer?

    init(font: UIFont) {
        super.init(frame: .zero, font: font)

        let edge = UIScreenEdgePanGestureRecognizer(target: self, action: #selector(edgeSwiped))
        edge.edges = .left
        for other in gestureRecognizers ?? [] where other is UIPanGestureRecognizer {
            other.require(toFail: edge)
        }
        addGestureRecognizer(edge)
        self.edge = edge

        // SwiftTerm's own taps (keyboard, selection) wait until this one sees that the tap
        // is not on the label.
        let tap = UITapGestureRecognizer(target: self, action: #selector(goHome))
        for other in gestureRecognizers ?? [] where other is UITapGestureRecognizer {
            other.require(toFail: tap)
        }
        addGestureRecognizer(tap)
        labelTap = tap
    }

    required init?(coder: NSCoder) { nil }

    /// SwiftTerm adds its mouse and selection pans only once it needs them: they wait for the
    /// edge swipe too, or a swipe with the tmux mouse on would drag in the pane.
    override func addGestureRecognizer(_ gesture: UIGestureRecognizer) {
        super.addGestureRecognizer(gesture)
        if let edge, gesture is UIPanGestureRecognizer, gesture !== edge {
            gesture.require(toFail: edge)
        }
    }

    override var keyCommands: [UIKeyCommand]? {
        let home = UIKeyCommand(title: "Back to matchblox", action: #selector(goHome), input: "m", modifierFlags: .command)
        home.wantsPriorityOverSystemBehavior = true
        return (super.keyCommands ?? []) + [home]
    }

    override func gestureRecognizerShouldBegin(_ gesture: UIGestureRecognizer) -> Bool {
        guard gesture === labelTap else { return super.gestureRecognizerShouldBegin(gesture) }
        return tapsLabel(at: gesture.location(in: self))
    }

    @objc private func edgeSwiped(_ edge: UIScreenEdgePanGestureRecognizer) {
        switch edge.state {
        case .began:
            haptic.prepare()
        case .ended where edge.translation(in: self).x > 60 || edge.velocity(in: self).x > 500:
            goHome()
        default:
            break
        }
    }

    @objc private func goHome() {
        haptic.impactOccurred()
        onHome()
    }

    /// The status line is the first or the last row, and a finger may land a row or a column off.
    /// The cell size is SwiftTerm's own (`computeFontDimensions`), which it does not make public.
    /// Its content holds the whole buffer, so the visible row is the content row less `yDisp`.
    private func tapsLabel(at point: CGPoint) -> Bool {
        let terminal = getTerminal()
        let cellWidth = "W".size(withAttributes: [.font: font]).width
        let cellHeight = ceil((font.ascender - font.descender + font.leading) * lineSpacing)
        let col = Int(point.x / cellWidth)
        let row = Int(point.y / cellHeight) - terminal.buffer.yDisp
        for status in [0, terminal.rows - 1] where abs(row - status) <= 1 {
            let cells = (0..<terminal.cols).map { terminal.getCharacter(col: $0, row: status) ?? " " }
            if let label = HomeKey.labelColumns(in: cells), (label.lowerBound - 1...label.upperBound).contains(col) {
                return true
            }
        }
        return false
    }
}
