import MatchbloxKit
import SwiftUI

@main
struct MatchbloxApp: App {
    private let store = HostStore.standard
    @State private var route = Opening.first(hosts: HostStore.standard.all())
    @State private var scanProblem: String?
    @State private var scanNotice: String?

    var body: some Scene {
        WindowGroup {
            ZStack {
                Theme.bg.ignoresSafeArea()
                switch route {
                case .scan:
                    ScanView(
                        problem: scanProblem,
                        notice: scanNotice,
                        onCode: open,
                        onCancel: store.all().isEmpty ? nil : { go(Opening.first(hosts: store.all())) }
                    )
                case .pair(let draft):
                    PairingView(
                        draft: draft,
                        store: store,
                        onPaired: { host in
                            // The console slides in from the drawer side.
                            withAnimation(UIAccessibility.isReduceMotionEnabled ? nil : .easeOut(duration: 0.3)) {
                                go(.console(host))
                            }
                        },
                        onScanAgain: { go(.scan) }
                    )
                    .id(draft.payload.key)
                case .console(let host):
                    ConsoleView(
                        host: host,
                        onFailure: { go(.reach(host, $0)) },
                        onHosts: store.all().count > 1 ? { go(.hosts) } : nil,
                        onScanAgain: { go(.scan) }
                    )
                    .transition(.move(edge: .trailing))
                    .zIndex(1)
                case .hosts:
                    HostsView(
                        store: store,
                        onOpen: { go(.console($0)) },
                        onPair: { go(.scan) },
                        onNoHosts: { notice in
                            go(.scan)
                            scanNotice = notice
                        }
                    )
                case .reach(let host, let failure):
                    ReachView(
                        host: host,
                        failure: failure,
                        store: store,
                        onRetry: { go(.console($0)) },
                        onScanAgain: { go(.scan) }
                    )
                }
            }
            .foregroundStyle(Theme.fg)
            .fontDesign(.monospaced)
            .tint(Theme.accent)
            .preferredColorScheme(.dark)
            .onOpenURL(perform: open)
        }
    }

    private func go(_ next: Route) {
        scanProblem = nil
        scanNotice = nil
        route = next
    }

    /// An open console gives way only to a new pairing code.
    private func open(_ url: URL) {
        if case .console = route, url.host?.lowercased() != "pair" { return }
        do {
            let payload = try PairPayload(url: url)
            go(.pair(PairDraft(payload: payload, phoneName: Pairing.cleanName(UIDevice.current.name))))
        } catch {
            scanProblem = Pairing.message(for: error)
            route = .scan
        }
    }
}
