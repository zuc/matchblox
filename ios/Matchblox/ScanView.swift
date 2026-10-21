import AVFoundation
import MatchbloxKit
import SwiftUI
import UIKit
import VisionKit

struct ScanView: View {
    let problem: String?
    let notice: String?
    let onCode: (URL) -> Void
    let onCancel: (() -> Void)?

    private enum Camera {
        case checking, ready, unsupported, denied
    }

    @State private var camera = Camera.checking
    /// The code's bounds, once the scanner finds it.
    @State private var lock: CGRect?
    @State private var locking: Task<Void, Never>?
    @State private var notALink = false
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var hasCamera: Bool { camera == .checking || camera == .ready }

    var body: some View {
        ZStack {
            if camera == .ready {
                QRScanner(armed: lock == nil, onCode: caught)
                    .ignoresSafeArea()
            }
            GeometryReader { geo in
                let box = PairStage.boxFrame(in: geo.size)
                MatchBox(pose: hasCamera ? .outline : .shut)
                    .frame(width: box.width, height: box.height)
                    .modifier(LockMove(box: box, code: lock))
                    .position(x: box.midX, y: box.midY)
            }
            .ignoresSafeArea()
            .allowsHitTesting(false)
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Text("Pair a host")
                        .font(.title3)
                    Spacer()
                    if let onCancel {
                        Button("Cancel", action: onCancel)
                            .foregroundStyle(Theme.muted)
                    }
                }
                .modifier(OverCamera(on: camera == .ready))
                Spacer()
                bottom
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(24)
        }
        .onAppear { Haptics.prepare() }
        .onDisappear { locking?.cancel() }
        // The scanner arms. The lock haptic is then ready to play on time.
        .onChange(of: camera == .ready && lock == nil) { _, armed in
            if armed { Haptics.prepare() }
        }
        .task { camera = await Self.cameraState() }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active, camera == .denied else { return }
            Task { camera = await Self.cameraState() }
        }
    }

    private var bottom: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let notice {
                Text(notice)
                    .foregroundStyle(Theme.muted)
            }
            if let problem {
                Text(problem)
                    .foregroundStyle(Theme.neg)
            }
            switch camera {
            case .checking, .ready:
                Text("Run matchblox pair on your host, or open Pair a device in its console.")
                    .foregroundStyle(Theme.muted)
            case .unsupported:
                Text("This iPhone can't scan codes in matchblox. Open the Camera app and point it at the code. The Camera opens matchblox.")
            case .denied:
                Text("matchblox can't use the camera. Allow the camera in Settings, or scan the code with the Camera app.")
                Button("Open Settings") {
                    if let settings = URL(string: UIApplication.openSettingsURLString) {
                        UIApplication.shared.open(settings)
                    }
                }
            }
            PasteButton(payloadType: String.self) { texts in
                let link = texts.lazy.compactMap(Pairing.firstLink(in:)).first
                Task { @MainActor in pasted(link) }
            }
            .buttonBorderShape(.capsule)
            if notALink {
                Text("That is not a matchblox pair link. Copy it again with c on the pair page.")
                    .foregroundStyle(Theme.neg)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .modifier(OverCamera(on: camera == .ready))
    }

    /// A pasted link has no lock to show: it goes straight to the box.
    private func pasted(_ link: URL?) {
        notALink = link == nil
        if let link { onCode(link) }
    }

    /// The outline moves to the code, then the box takes the link.
    private func caught(_ url: URL, _ bounds: CGRect) {
        Haptics.lock()
        withAnimation(reduceMotion ? nil : .easeOut(duration: PairStage.lock)) {
            lock = bounds
        }
        locking = Task {
            try? await Task.sleep(for: .seconds(PairStage.lock))
            guard !Task.isCancelled else { return }
            onCode(url)
            // A link that does not open stays on this screen with its problem. The outline goes back.
            lock = nil
        }
    }

    @MainActor
    private static func cameraState() async -> Camera {
        guard DataScannerViewController.isSupported else { return .unsupported }
        if AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined {
            _ = await AVCaptureDevice.requestAccess(for: .video)
        }
        return DataScannerViewController.isAvailable ? .ready : .denied
    }
}

/// The place of the box on the scan and pair screens. Both screens use it, so the box stays still from one to the other.
enum PairStage {
    /// The lock, in seconds (design/0009 §6.2).
    static let lock = 0.18

    /// The box's frame, centred in a full-screen view of this size.
    static func boxFrame(in size: CGSize) -> CGRect {
        let width = max(min(size.width - 48, size.height * 0.45 * MatchBox.aspectRatio, 440), 1)
        let height = width / MatchBox.aspectRatio
        return CGRect(x: (size.width - width) / 2, y: (size.height - height) / 2, width: width, height: height)
    }
}

/// A `bg` backing at 0.8, so that text stays legible over a bright camera.
private struct OverCamera: ViewModifier {
    let on: Bool

    func body(content: Content) -> some View {
        content
            .padding(on ? 16 : 0)
            .background(Theme.bg.opacity(on ? 0.8 : 0), in: RoundedRectangle(cornerRadius: 12))
    }
}

/// Scales and moves the box so that its window covers the code.
private struct LockMove: ViewModifier {
    let box: CGRect
    let code: CGRect?

    func body(content: Content) -> some View {
        let window = MatchBox.window(in: box.size).offsetBy(dx: box.minX, dy: box.minY)
        let k = code.map { max($0.width, $0.height) / window.width } ?? 1
        let target = code.map { CGPoint(x: $0.midX, y: $0.midY) } ?? CGPoint(x: window.midX, y: window.midY)
        // The scale turns about the box's centre. The move then puts the window's centre on the code's.
        content
            .scaleEffect(k)
            .offset(
                x: target.x - box.midX - k * (window.midX - box.midX),
                y: target.y - box.midY - k * (window.midY - box.midY)
            )
    }
}

/// Reports only `matchblox://pair` URLs, with the bounds of the code, and ignores every other code.
/// It reports nothing while it is not armed, and never the same code twice.
private struct QRScanner: UIViewControllerRepresentable {
    let armed: Bool
    let onCode: (URL, CGRect) -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(onCode: onCode)
    }

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let scanner = DataScannerViewController(
            recognizedDataTypes: [.barcode(symbologies: [.qr])],
            qualityLevel: .balanced,
            recognizesMultipleItems: false,
            isHighFrameRateTrackingEnabled: false,
            isHighlightingEnabled: false
        )
        scanner.delegate = context.coordinator
        return scanner
    }

    func updateUIViewController(_ scanner: DataScannerViewController, context: Context) {
        context.coordinator.onCode = onCode
        context.coordinator.armed = armed
        if !scanner.isScanning {
            try? scanner.startScanning()
        }
    }

    static func dismantleUIViewController(_ scanner: DataScannerViewController, coordinator: Coordinator) {
        scanner.stopScanning()
    }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        var onCode: (URL, CGRect) -> Void
        var armed = true
        private var last: String?

        init(onCode: @escaping (URL, CGRect) -> Void) {
            self.onCode = onCode
        }

        func dataScanner(
            _ dataScanner: DataScannerViewController,
            didAdd addedItems: [RecognizedItem],
            allItems: [RecognizedItem]
        ) {
            guard armed else { return }
            for item in addedItems {
                guard case .barcode(let code) = item,
                      let text = code.payloadStringValue,
                      let url = URL(string: text),
                      url.scheme?.lowercased() == "matchblox",
                      url.host?.lowercased() == "pair",
                      text != last
                else { continue }
                last = text
                let b = code.bounds
                let xs = [b.topLeft.x, b.topRight.x, b.bottomRight.x, b.bottomLeft.x]
                let ys = [b.topLeft.y, b.topRight.y, b.bottomRight.y, b.bottomLeft.y]
                onCode(url, CGRect(x: xs.min()!, y: ys.min()!, width: xs.max()! - xs.min()!, height: ys.max()! - ys.min()!))
                return
            }
        }
    }
}
