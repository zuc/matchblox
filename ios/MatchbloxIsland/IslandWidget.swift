import ActivityKit
import MatchbloxKit
import SwiftUI
import WidgetKit

/// What one render of the island needs: the state, the face the setting picks, and the
/// names the phone keeps for the numbers.
struct IslandModel {
    let state: IslandState
    let face: Face
    let names: IslandNames
    /// The host's short name, or its 8 characters when the phone has none.
    let label: String
    let now: Date

    /// Nil on the face `nothing`: the widget URL and the buttons open nothing there.
    var link: URL? { IslandLook.link(state, face: face) }
}

/// Reads the Island setting and the names from the App Group, where the app keeps them.
/// A Live Activity does not draw again when these change: the app updates the activity.
enum IslandStore {
    static let appGroup = Bundle.main.object(forInfoDictionaryKey: "MatchbloxAppGroup") as? String ?? ""
    private static var defaults: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }

    static var prefs: IslandPrefs {
        guard let data = defaults.data(forKey: IslandShelf.prefsKey),
              let prefs = try? JSONDecoder().decode(IslandPrefs.self, from: data)
        else { return IslandPrefs() }
        return prefs
    }

    static func names(host: String) -> IslandNames {
        guard let dir = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup),
              let data = try? Data(contentsOf: dir.appendingPathComponent(IslandNames.fileName(host: host))),
              let names = try? JSONDecoder().decode(IslandNames.self, from: data)
        else { return IslandNames() }
        return names
    }

    static func label(host: String) -> String {
        defaults.string(forKey: IslandShelf.labelKey(host: host)) ?? host
    }

    /// The model of one render. `now` is the time of the render: the system renders again
    /// at the content's stale date, the end of a wait's break-through.
    static func model(_ state: IslandState, now: Date = .now) -> IslandModel {
        if IslandPreview.running { return IslandPreview.model(state, now: now) }
        let prefs = prefs
        let face = face(for: state, show: prefs.show, followed: IslandShelf.followed(prefs), now: now)
        return IslandModel(state: state, face: face, names: names(host: state.host), label: label(host: state.host), now: now)
    }
}

struct MatchbloxIslandActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: IslandAttributes.self) { context in
            let m = IslandStore.model(context.state)
            LockFace(m)
                .activityBackgroundTint(Tokens.bg.opacity(0.78))
                .activitySystemActionForegroundColor(Tokens.fg)
                .widgetURL(m.link)
        } dynamicIsland: { context in
            let m = IslandStore.model(context.state)
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    if m.face != .nothing { HostLine(label: m.label).padding(.leading, 6) }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    switch m.face {
                    case .sessions: Counts(burn: m.state.burn, wait: m.state.wait).padding(.trailing, 6)
                    case .usage: UsageCounts(state: m.state).padding(.trailing, 6)
                    case .nothing: EmptyView()
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    switch m.face {
                    case .sessions: SessionsExpanded(m)
                    case .usage: UsageExpanded(m)
                    case .nothing: EmptyView()
                    }
                }
            } compactLeading: {
                switch m.face {
                case .sessions: SessionsCompactLeading(m)
                case .usage: UsageCompactLeading(m)
                case .nothing: EmptyView()
                }
            } compactTrailing: {
                switch m.face {
                case .sessions: SessionsCompactTrailing(m)
                case .usage: UsageCompactTrailing(m)
                case .nothing: EmptyView()
                }
            } minimal: {
                switch m.face {
                case .sessions: SessionsMinimal(m)
                case .usage: UsageMinimal(m)
                case .nothing: EmptyView()
                }
            }
            .widgetURL(m.link)
            .keylineTint(Tokens.accent)
        }
    }
}

/// The lock screen: one face or the other; nothing for a host that is not followed.
struct LockFace: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        switch m.face {
        case .sessions: SessionsLock(m)
        case .usage: UsageLock(m)
        case .nothing: EmptyView()
        }
    }
}

// MARK: shared pieces

enum IslandFont {
    static let mono = Font.system(size: 13, weight: .medium, design: .monospaced)
    static let monoSmall = Font.system(size: 12, weight: .medium, design: .monospaced)
    static let name = Font.system(size: 15, weight: .semibold)
    static let compact = Font.system(size: 12, weight: .medium)
}

/// `▰ ws-1`: the host, with its mark in the accent.
struct HostLine: View {
    let label: String
    var body: some View {
        (Text("▰ ").foregroundColor(Tokens.accent) + Text(label).foregroundColor(Tokens.fg))
            .font(IslandFont.mono)
            .lineLimit(1)
    }
}

/// `✦ 4  ╿ 1`: the burning and the waiting sessions; a zero shows nothing.
struct Counts: View {
    let burn: Int
    let wait: Int
    var body: some View {
        HStack(spacing: 10) {
            if burn > 0 { Text("✦ \(burn)").foregroundStyle(Tokens.flameAmber) }
            if wait > 0 { Text("╿ \(wait)").foregroundStyle(Tokens.accent) }
        }
        .font(IslandFont.mono)
    }
}

extension Level {
    /// A bar's fill: the accent below 60 %, then warn and neg.
    var fill: Color {
        switch self {
        case .normal: Tokens.accent
        case .warn: Tokens.warn
        case .neg: Tokens.neg
        }
    }

    /// A percent: plain text below 60 %, else the colour of its bar.
    var text: Color { self == .normal ? Tokens.fg : fill }
}

@main
struct MatchbloxIslandBundle: WidgetBundle {
    var body: some Widget {
        MatchbloxIslandActivity()
    }
}
