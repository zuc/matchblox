/// The way back to the console: the key the host binds to it (`panes.HomeKey`), and the
/// label the host's status line shows for it (`panes.WayBack`).
public enum HomeKey {
    public static let bytes = Array("\u{1b}_matchblox\u{1b}\\".utf8)
    public static let label = "◂ matchblox"

    /// The columns of the label in a terminal row, one character for each cell.
    public static func labelColumns(in cells: [Character]) -> Range<Int>? {
        let label = Array(label)
        guard cells.count >= label.count else { return nil }
        let start = (0...(cells.count - label.count)).first { cells[$0..<$0 + label.count].elementsEqual(label) }
        return start.map { $0..<$0 + label.count }
    }
}
