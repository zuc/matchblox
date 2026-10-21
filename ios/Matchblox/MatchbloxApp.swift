import MatchbloxKit
import SwiftUI
import UserNotifications

/// Asks iOS for the push tokens at launch and ends stray activities. Nothing here connects to a host.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        MainActor.assumeIsolated {
            PushTokens.shared.begin()
            IslandSettings.endStrayActivities()
        }
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let token = hexToken(deviceToken)
        MainActor.assumeIsolated {
            PushTokens.shared.device = token
            IslandSettings.keptDeviceToken = token
        }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        islandLog.error("no device token: \(String(describing: error), privacy: .public)")
    }

    /// A tapped buzz opens its session through the link handling of a tapped island:
    /// `userInfo["open"]`, which the buzz extension sets, else the buzz's host and number.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo
        let url = (userInfo["open"] as? String).flatMap(URL.init(string:)).flatMap(OpenLink.init(url:))?.url
            ?? BuzzPayload.event(userInfo).flatMap { OpenLink(host: $0.host, session: $0.n) }?.url
        if let url {
            Task { @MainActor in UIApplication.shared.open(url) }
        }
        completionHandler()
    }

    /// A buzz that comes while the app is open shows as a banner too.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }
}

@main
struct MatchbloxApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    private let store = HostStore.standard
    /// A tapped link opens a new console even when one is open.
    @State private var linkOpens = 0
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
                                go(.console(host, open: nil))
                            }
                        },
                        onScanAgain: { go(.scan) }
                    )
                    .id(draft.payload.key)
                case .console(let host, let session):
                    ConsoleView(
                        host: host,
                        open: session,
                        onFailure: { go(.reach(host, $0)) },
                        onHosts: store.all().count > 1 ? { go(.hosts) } : nil,
                        onIsland: { go(.island) },
                        onScanAgain: { go(.scan) }
                    )
                    .id("\(host.fingerprint)#\(linkOpens)")
                    .transition(.move(edge: .trailing))
                    .zIndex(1)
                case .hosts:
                    HostsView(
                        store: store,
                        onOpen: { go(.console($0, open: nil)) },
                        onPair: { go(.scan) },
                        onIsland: { go(.island) },
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
                        onRetry: { go(.console($0, open: nil)) },
                        onScanAgain: { go(.scan) }
                    )
                case .island:
                    IslandView(store: store, onBack: { go(Opening.first(hosts: store.all())) })
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

    /// A tapped session (`matchblox://open`) opens that host's console after Face ID; an
    /// open console gives way to it and to a new pairing code only.
    private func open(_ url: URL) {
        if url.host == "open" {
            if let link = OpenLink(url: url) {
                linkOpens += 1
                go(Opening.route(for: link, hosts: store.all()))
            } else if case .console = route {
                return
            } else {
                go(Opening.first(hosts: store.all()) == .scan ? .scan : .hosts)
            }
            return
        }
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
