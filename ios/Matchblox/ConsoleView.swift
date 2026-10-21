import LocalAuthentication
import MatchbloxKit
import NIOSSH
import SwiftTerm
import SwiftUI

/// The console window: `matchblox console` on the host, drawn by SwiftTerm.
/// A failed connect goes to `onFailure`; the router shows the screen that fixes it.
struct ConsoleView: View {
    let host: Host
    let onHosts: (() -> Void)?
    let onScanAgain: () -> Void

    @State private var connection: ConsoleConnection
    @Environment(\.scenePhase) private var scenePhase

    init(
        host: Host,
        onFailure: @escaping (ConnectFailure) -> Void,
        onHosts: (() -> Void)?,
        onScanAgain: @escaping () -> Void
    ) {
        self.host = host
        self.onHosts = onHosts
        self.onScanAgain = onScanAgain
        _connection = State(initialValue: ConsoleConnection(host: host, onFailure: onFailure))
    }

    var body: some View {
        ZStack {
            TerminalRepresentable(connection: connection)
            if connection.phase != .live {
                cover
            }
        }
        .onAppear { connection.start() }
        .onDisappear { connection.stop() }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .background: connection.stop()
            case .active: connection.start()
            default: break
            }
        }
    }

    private var cover: some View {
        VStack(alignment: .leading, spacing: 20) {
            switch connection.phase {
            case .connecting, .live:
                Text("Connecting to \(host.label)")
                    .foregroundStyle(Theme.muted)
                hostsButton
            case .failed(let message, let scanAgain):
                Text(message)
                    .foregroundStyle(Theme.neg)
                HStack(spacing: 24) {
                    if scanAgain {
                        Button("Scan again", action: onScanAgain)
                            .buttonStyle(.borderedProminent)
                            .foregroundStyle(Theme.bg)
                    } else {
                        Button("Try again") { connection.retry() }
                            .buttonStyle(.borderedProminent)
                            .foregroundStyle(Theme.bg)
                    }
                    hostsButton
                }
            case .ended:
                Text(Opening.endedMessage(host: host.label))
                    .foregroundStyle(Theme.muted)
                HStack(spacing: 24) {
                    Button("Open again") { connection.retry() }
                        .buttonStyle(.borderedProminent)
                        .foregroundStyle(Theme.bg)
                    hostsButton
                }
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .padding(24)
        .background(Theme.bg)
    }

    @ViewBuilder
    private var hostsButton: some View {
        if let onHosts {
            Button("Hosts", action: onHosts)
                .foregroundStyle(Theme.muted)
        }
    }
}

private struct TerminalRepresentable: UIViewRepresentable {
    let connection: ConsoleConnection

    func makeUIView(context: Context) -> TerminalView {
        let terminal = TerminalView(frame: .zero, font: UIFont.monospacedSystemFont(ofSize: 13, weight: .regular))
        let bg = UIColor(Theme.bg)
        terminal.backgroundColor = bg
        terminal.nativeBackgroundColor = bg
        terminal.nativeForegroundColor = UIColor(Theme.fg)
        terminal.caretColor = UIColor(Theme.accent)
        terminal.keyboardAppearance = .dark
        terminal.contentInsetAdjustmentBehavior = .never
        terminal.terminalDelegate = connection
        connection.terminal = terminal
        return terminal
    }

    func updateUIView(_ terminal: TerminalView, context: Context) {}
}

/// Owns one SSHSession for each connect. A session that ends at once opens again; a good session that ends waits for a tap. It stops when the view goes away or the app goes to the background.
@MainActor
@Observable
final class ConsoleConnection {
    enum Phase: Equatable {
        case connecting
        case live
        case ended
        case failed(String, scanAgain: Bool)
    }

    private enum Outgoing: Sendable {
        case input(Data)
        case resize(cols: Int, rows: Int)
    }

    private enum Ending {
        case failed(ConnectFailure)
        case ended(sawOutput: Bool)
    }

    private struct UnlockFailed: Error {
        let message: String
    }

    private(set) var phase = Phase.connecting

    @ObservationIgnored weak var terminal: TerminalView?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var outgoing: AsyncStream<Outgoing>.Continuation?
    /// One unlock serves every reconnect until the app goes to the background.
    @ObservationIgnored private var unlocked: LAContext?
    private let host: Host
    private let onFailure: (ConnectFailure) -> Void

    init(host: Host, onFailure: @escaping (ConnectFailure) -> Void) {
        self.host = host
        self.onFailure = onFailure
    }

    func start() {
        guard task == nil else { return }
        phase = .connecting
        task = Task { await run() }
    }

    func stop() {
        cancel()
        unlocked?.invalidate()
        unlocked = nil
    }

    func retry() {
        cancel()
        start()
    }

    private func cancel() {
        task?.cancel()
        task = nil
        outgoing?.finish()
        outgoing = nil
        phase = .connecting
    }

    private func run() async {
        var delay = ReconnectDelay()
        while !Task.isCancelled {
            phase = .connecting
            let key: NIOSSHPrivateKey
            do {
                key = try await unlockKey()
            } catch let failure as UnlockFailed {
                guard !Task.isCancelled else { return }
                phase = .failed(failure.message, scanAgain: false)
                return
            } catch {
                guard !Task.isCancelled else { return }
                let keyError = error as? DeviceKeyError ?? .keyUnavailable
                phase = .failed(keyError.localizedDescription, scanAgain: keyError == .keyUnavailable)
                return
            }
            guard !Task.isCancelled else { return }

            let session = SSHSession(
                host: host.address,
                port: host.port,
                user: host.user,
                key: key,
                hostKeyFingerprint: host.fingerprint
            )
            let opened = ContinuousClock.now
            let ending = await withTaskCancellationHandler {
                await serve(session)
            } onCancel: {
                Task { await session.close() }
            }
            await session.close()
            guard !Task.isCancelled else { return }
            switch ending {
            case .failed(let failure):
                onFailure(failure)
                return
            case .ended(let sawOutput):
                switch delay.next(after: ContinuousClock.now - opened, sawOutput: sawOutput) {
                case .reconnect(let wait):
                    phase = .connecting
                    try? await Task.sleep(for: wait)
                case .ended:
                    phase = .ended
                    return
                case .stop:
                    phase = .failed(Opening.stopsAtOnceMessage(host: host.label), scanAgain: false)
                    return
                }
            }
        }
    }

    /// Runs one session until its stream ends.
    private func serve(_ session: SSHSession) async -> Ending {
        let size = terminalSize
        let output: AsyncStream<Data>
        do {
            output = try await session.open(command: "matchblox console", pty: size)
        } catch {
            return .failed(error)
        }
        guard !Task.isCancelled else { return .ended(sawOutput: false) }

        let (input, sink) = AsyncStream<Outgoing>.makeStream()
        outgoing = sink
        let writer = Task {
            for await item in input {
                switch item {
                case .input(let data):
                    await session.write(data)
                case .resize(let cols, let rows):
                    await session.resize(cols: cols, rows: rows)
                }
            }
        }
        defer {
            sink.finish()
            writer.cancel()
        }

        terminal?.getTerminal().resetToInitialState()
        phase = .live
        let now = terminalSize
        if now != size {
            sink.yield(.resize(cols: now.cols, rows: now.rows))
        }
        var sawOutput = false
        for await chunk in output {
            sawOutput = true
            terminal?.feed(byteArray: ArraySlice(chunk))
        }
        return .ended(sawOutput: sawOutput)
    }

    private var terminalSize: (cols: Int, rows: Int) {
        guard let terminal = terminal?.getTerminal() else { return (80, 25) }
        return (terminal.cols, terminal.rows)
    }

    /// Face ID (or the passcode) runs here, before SSHSession starts its deadline. The unlocked context then lets the key sign with no second prompt.
    private func unlockKey() async throws -> NIOSSHPrivateKey {
        let context: LAContext
        if let unlocked {
            context = unlocked
        } else {
            context = LAContext()
            context.touchIDAuthenticationAllowableReuseDuration = 10
            try await Self.evaluate(context, reason: "Open the console of \(host.label).")
            guard !Task.isCancelled else {
                context.invalidate()
                throw CancellationError()
            }
            unlocked = context
        }
        return DeviceKey.nioKey(try DeviceKey.load(host: host.fingerprint, context: context))
    }

    private static func evaluate(_ context: LAContext, reason: String) async throws {
        try await withCheckedThrowingContinuation { (done: CheckedContinuation<Void, Error>) in
            context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) { @Sendable success, error in
                if success {
                    done.resume()
                } else {
                    done.resume(throwing: UnlockFailed(message: unlockMessage(for: error)))
                }
            }
        }
    }

    private nonisolated static func unlockMessage(for error: Error?) -> String {
        switch (error as? LAError)?.code {
        case .userCancel, .systemCancel, .appCancel:
            "The key is still locked. Tap Try again, then use Face ID or the passcode."
        case .passcodeNotSet:
            DeviceKeyError.noPasscode.localizedDescription
        default:
            "This device could not check Face ID or the passcode. Tap Try again."
        }
    }
}

extension ConsoleConnection: @preconcurrency TerminalViewDelegate {
    func send(source: TerminalView, data: ArraySlice<UInt8>) {
        outgoing?.yield(.input(Data(data)))
    }

    func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {
        outgoing?.yield(.resize(cols: newCols, rows: newRows))
    }

    func setTerminalTitle(source: TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    func scrolled(source: TerminalView, position: Double) {}
    func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {}
    func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}
