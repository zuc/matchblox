import MatchbloxKit
import SwiftUI
import WidgetKit

// The Usage face (design/0010-island-usage.html): will the limits hold? Bars as the
// console's dock draws them. Nothing here moves; the compact island keeps the match of
// the top session on the left, beside the tightest window: the split.

struct UsageLock: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                HostLine(label: m.label)
                Spacer(minLength: 10)
                UsageCounts(state: m.state)
            }
            ForEach(IslandLook.shown(m.state), id: \.n) { a in
                AccountBlock(account: a, names: m.names, now: m.now)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
    }
}

struct UsageExpanded: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(IslandLook.shown(m.state), id: \.n) { a in
                AccountBlock(account: a, names: m.names, now: m.now)
            }
            if let link = m.link {
                HStack {
                    Spacer()
                    IslandButton(title: "Open", link: link)
                }
                .padding(.top, 4)
            }
        }
        .padding(.horizontal, 6)
    }
}

/// The top line's right: `╿ 1` while anything waits (after the break-through too), and
/// `stoker` in the night token while it runs.
struct UsageCounts: View {
    let state: IslandState

    var body: some View {
        HStack(spacing: 10) {
            if state.wait > 0 { Text("╿ \(state.wait)").foregroundStyle(Tokens.accent) }
            if state.stoker { Text("stoker").foregroundStyle(Tokens.night) }
        }
        .font(IslandFont.mono)
    }
}

/// The split's left: the burning match of the top session, as on the Sessions compact;
/// the host mark `▰` when nothing is on.
struct UsageCompactLeading: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        if IslandLook.anythingOn(m.state) {
            SessionsCompactLeading(m)
        } else {
            Text("▰")
                .font(IslandFont.mono)
                .foregroundStyle(Tokens.accent)
                .padding(.leading, 4)
        }
    }
}

/// The split's right: the tightest window over all accounts, `╿` before it while anything waits.
struct UsageCompactTrailing: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        if let c = IslandLook.usageCompact(m.state) {
            HStack(spacing: 4) {
                if c.waits { Text("╿").foregroundStyle(Tokens.accent) }
                Text(c.text).foregroundStyle(barColorLevel(c.pct).text)
            }
            .font(IslandFont.compact)
            .lineLimit(1)
        }
    }
}

/// A short bar of the tightest window, with `╿` above it while a session waits.
struct UsageMinimal: View {
    let m: IslandModel
    init(_ m: IslandModel) { self.m = m }

    var body: some View {
        VStack(spacing: 2) {
            if m.state.wait > 0 {
                Text("╿")
                    .font(.system(size: 9, weight: .medium, design: .monospaced))
                    .foregroundStyle(Tokens.accent)
            }
            if let w = tightest(m.state) {
                ZStack(alignment: .leading) {
                    Capsule().fill(Tokens.faint.opacity(0.5))
                    Capsule().fill(barColorLevel(w.pct).fill)
                        .frame(width: 22 * CGFloat(min(max(w.pct, 0), 100)) / 100)
                }
                .frame(width: 22, height: 4)
            } else {
                Text("▰").font(IslandFont.mono).foregroundStyle(Tokens.accent)
            }
        }
    }
}

/// One account: its name and forecast, then a line for each window.
struct AccountBlock: View {
    let account: AccountUse
    let names: IslandNames
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Rectangle().fill(Tokens.hair).frame(height: 1)
                .padding(.bottom, 3)
            HStack(alignment: .firstTextBaseline) {
                Text(names.account(account.n))
                    .font(IslandFont.name)
                    .foregroundStyle(Tokens.fg)
                    .lineLimit(1)
                Spacer(minLength: 10)
                if let f = IslandLook.forecast(account, now: now) {
                    Text(f.text)
                        .font(IslandFont.monoSmall)
                        .foregroundStyle(f.out ? Tokens.warn : Tokens.muted)
                        .lineLimit(1)
                }
            }
            ForEach(account.windows, id: \.name) { w in
                WindowLine(window: w, now: now)
            }
        }
    }
}

/// `5h ▰▰▰▰▰▱▱▱ 61% · 14:10`: the fill in the accent below 60 %, warn from 60, neg from 90.
struct WindowLine: View {
    let window: WindowUse
    let now: Date

    var body: some View {
        let level = barColorLevel(window.pct)
        let cells = IslandLook.cells(window.pct)
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(window.name)
                .foregroundStyle(Tokens.muted)
                .frame(width: 22, alignment: .leading)
            (Text(String(repeating: "▰", count: cells.filled)).foregroundColor(level.fill)
                + Text(String(repeating: "▱", count: cells.empty)).foregroundColor(Tokens.faint))
            Text("\(window.pct)%")
                .foregroundStyle(level.text)
                .frame(width: 36, alignment: .trailing)
            if let r = IslandLook.resets(window, now: now) {
                Text("· \(r)")
                    .foregroundStyle(Tokens.muted)
                    .lineLimit(1)
            }
        }
        .font(IslandFont.monoSmall)
    }
}
