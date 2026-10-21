import MatchbloxKit
import NIOSSH
import SwiftUI

/// The box takes the link and pairs, with no form: the scan is the consent (design/0009 §6.2, §6.5).
struct PairingView: View {
    @State var draft: PairDraft
    let store: HostStore
    let onPaired: (Host) -> Void
    let onScanAgain: () -> Void

    private enum Phase: Equatable {
        case pairing
        case paired(Date)
        /// `retry` is true when another address can help.
        case failed(String, retry: Bool)
    }

    /// The burning match stays this long before the console comes: one flame, in seconds.
    private static let hold = 0.24
    private static let actions = "actions"

    @State private var phase = Phase.pairing
    @State private var attempt = 0
    @State private var pulse = false
    /// The full screen. The box takes its place from it, so the keyboard does not move the box.
    @State private var screen = CGSize.zero
    @FocusState private var editing: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var pose: BoxPose {
        switch phase {
        case .pairing: .shut
        case .paired(let start): .open(start)
        case .failed: .failed
        }
    }

    var body: some View {
        let box = PairStage.boxFrame(in: screen)
        ZStack {
            MatchBox(pose: pose, label: AnyView(label))
                .frame(width: box.width, height: box.height)
                .position(x: box.midX, y: box.midY)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .onGeometryChange(for: CGSize.self, of: \.size) { screen = $0 }
                .ignoresSafeArea()
            if case .failed(let message, let retry) = phase {
                failure(message, retry: retry, under: box)
            }
        }
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .task(id: attempt) { await pair() }
        .onChange(of: phase == .pairing, initial: true) { _, pairing in
            guard pairing, !reduceMotion else {
                pulse = false
                return
            }
            pulse = false
            withAnimation(.easeInOut(duration: 0.8).repeatForever(autoreverses: true)) {
                pulse = true
            }
        }
    }

    /// The host's name on the label. It pulses while the app connects: a value that is not known yet.
    private var label: some View {
        ZStack {
            Theme.fg
            Text(draft.payload.name)
                .font(.headline)
                .multilineTextAlignment(.center)
                .lineLimit(3)
                .minimumScaleFactor(0.4)
                .padding(6)
                .foregroundStyle(phase == .pairing || isPaired ? Theme.bg : Theme.faint)
                .opacity(phase == .pairing && pulse ? 0.4 : 1)
        }
    }

    private var isPaired: Bool {
        if case .paired = phase { return true }
        return false
    }

    /// The failure starts under the box, as wide as the box. It scrolls, so the keyboard never hides the field or Try again.
    private func failure(_ message: String, retry: Bool, under box: CGRect) -> some View {
        GeometryReader { view in
            let top = view.frame(in: .global).minY
            ScrollViewReader { scroll in
                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        Text(message)
                            .foregroundStyle(Theme.neg)
                        if retry {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("Address")
                                    .font(.footnote)
                                    .foregroundStyle(Theme.faint)
                                TextField("Address", text: $draft.address)
                                    .keyboardType(.URL)
                                    .focused($editing)
                                    .padding(.vertical, 8)
                                    .overlay(alignment: .bottom) {
                                        Rectangle().fill(Theme.hair).frame(height: 1)
                                    }
                            }
                        }
                        HStack(spacing: 24) {
                            if retry {
                                Button("Try again") {
                                    editing = false
                                    attempt += 1
                                }
                                .buttonStyle(.borderedProminent)
                                .foregroundStyle(Theme.bg)
                                .disabled(draft.address.trimmingCharacters(in: .whitespaces).isEmpty)
                            }
                            Button("Scan again", action: onScanAgain)
                                .foregroundStyle(Theme.muted)
                        }
                        .id(Self.actions)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 12)
                    // The block can scroll over the box. It stays legible there.
                    .background(Theme.bg)
                    .frame(width: box.width)
                    .frame(maxWidth: .infinity)
                    .padding(.top, max(box.maxY + 12 - top, 0))
                }
                .scrollDismissesKeyboard(.interactively)
                // The keyboard makes the view shorter. Try again then scrolls up above it.
                .onChange(of: view.size.height) {
                    guard editing else { return }
                    withAnimation { scroll.scrollTo(Self.actions, anchor: .bottom) }
                }
            }
        }
    }

    /// A new code or a new screen cancels the task, so an old pairing never opens a console.
    private func pair() async {
        Haptics.prepare()
        phase = .pairing
        let outcome = await PairFlow.run(draft, store: store)
        guard !Task.isCancelled else { return }
        // The flow can take many seconds, and the generators go idle in that time.
        Haptics.prepare()
        switch outcome {
        case .paired(let host):
            let clock = ContinuousClock()
            let start = clock.now
            phase = .paired(Date())
            do {
                try await clock.sleep(until: start + .seconds(MatchBox.strikeAt))
                Haptics.strike()
                try await clock.sleep(until: start + .seconds(MatchBox.stoodAt))
                Haptics.stood()
                try await clock.sleep(until: start + .seconds(MatchBox.stoodAt + Self.hold))
            } catch {
                return
            }
            onPaired(host)
        case .failed(let message, let retry):
            phase = .failed(message, retry: retry)
            Haptics.failed()
        }
    }
}

enum PairOutcome: Sendable {
    case paired(Host)
    /// `retry` is true when the host did not answer at the address, or another machine answered: another address can help.
    case failed(String, retry: Bool = false)
}

enum PairFlow {
    /// gate pair refuses a request after 30 s, so a host that says nothing by now never will.
    private static let replyDeadline = Duration.seconds(45)

    static func run(_ draft: PairDraft, store: HostStore) async -> PairOutcome {
        let payload = draft.payload
        let label = payload.name
        let address = draft.address.trimmingCharacters(in: .whitespacesAndNewlines)

        let phoneKey: String
        do {
            phoneKey = try await Task.detached { try DeviceKey.openSSH(for: payload.hostkey) }.value
        } catch {
            return .failed(error.localizedDescription)
        }

        let oneTimeKey: NIOSSHPrivateKey
        do {
            oneTimeKey = try SSHKeys.oneTimeKey(seedBase64: payload.key)
        } catch {
            return .failed(Pairing.message(for: .malformed))
        }

        let session = SSHSession(
            host: address,
            port: draft.port,
            user: payload.user,
            key: oneTimeKey,
            hostKeyFingerprint: payload.hostkey
        )
        let output: AsyncStream<Data>
        do {
            output = try await session.open(command: "matchblox gate pair", pty: nil)
        } catch {
            return .failed(
                Pairing.message(for: error, host: label, address: address),
                retry: error == .unreachable || error == .hostKeyChanged
            )
        }

        let request = Pairing.requestLine(name: Pairing.cleanName(draft.phoneName), key: phoneKey)
        let reply: PairReply
        do {
            reply = try await exchange(request, over: session, output: output)
        } catch {
            await session.close()
            return .failed(Pairing.message(for: error, host: label))
        }
        await session.close()

        switch reply {
        case .ok:
            let host = Host(
                label: label,
                address: address,
                port: draft.port,
                user: payload.user,
                fingerprint: payload.hostkey
            )
            let stale: [Host]
            do {
                stale = try store.upsert(host)
            } catch {
                return .failed("This iPhone could not save \(label). Free some space on the iPhone, then run matchblox pair on \(label) again.")
            }
            for old in stale {
                try? DeviceKey.remove(host: old.fingerprint)
            }
            return .paired(host)
        case .failed(let refusal):
            return .failed(Pairing.message(for: refusal, host: label))
        }
    }

    private static func exchange(
        _ request: Data,
        over session: SSHSession,
        output: AsyncStream<Data>
    ) async throws(PairReplyError) -> PairReply {
        let deadline = Task {
            try? await Task.sleep(for: replyDeadline)
            await session.close()
        }
        defer { deadline.cancel() }
        await session.write(request)
        return try await Pairing.readReply(from: output)
    }
}
