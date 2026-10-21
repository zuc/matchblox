import MatchbloxKit
import SwiftUI

/// Shown when a paired host does not open. It names the fix for the failure; the pairing stays unless the person scans again.
struct ReachView: View {
    let host: Host
    let failure: ConnectFailure
    let store: HostStore
    let onRetry: (Host) -> Void
    let onScanAgain: () -> Void

    @State private var address: String
    @State private var problem: String?

    init(host: Host, failure: ConnectFailure, store: HostStore, onRetry: @escaping (Host) -> Void, onScanAgain: @escaping () -> Void) {
        self.host = host
        self.failure = failure
        self.store = store
        self.onRetry = onRetry
        self.onScanAgain = onScanAgain
        _address = State(initialValue: host.address)
    }

    private var trimmedAddress: String { address.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            switch failure {
            case .unreachable:
                VStack(alignment: .leading, spacing: 6) {
                    Text(Opening.message(for: failure, host: host.label))
                        .font(.title3)
                    Text("New address?")
                        .foregroundStyle(Theme.muted)
                }
                TextField("Address", text: $address)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(.vertical, 8)
                    .overlay(alignment: .bottom) {
                        Rectangle().fill(Theme.hair).frame(height: 1)
                    }
                if let problem {
                    Text(problem)
                        .foregroundStyle(Theme.neg)
                }
                actions(retry: "Try again", canRetry: !trimmedAddress.isEmpty, action: retryWithAddress)
            case .closed:
                message
                actions(retry: "Try again", canRetry: true) { onRetry(host) }
            case .hostKeyChanged, .refused:
                message
                actions(retry: nil, canRetry: false) {}
            }
            Spacer()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(24)
    }

    private var message: some View {
        Text(Opening.message(for: failure, host: host.label))
            .font(.title3)
    }

    private func actions(retry: String?, canRetry: Bool, action: @escaping () -> Void) -> some View {
        HStack(spacing: 24) {
            if let retry {
                Button(retry, action: action)
                    .buttonStyle(.borderedProminent)
                    .foregroundStyle(Theme.bg)
                    .disabled(!canRetry)
            }
            Button("Scan again", action: onScanAgain)
                .foregroundStyle(retry == nil ? Theme.accent : Theme.muted)
        }
    }

    private func retryWithAddress() {
        do {
            try store.updateAddress(fingerprint: host.fingerprint, address: trimmedAddress)
        } catch {
            problem = "This device could not save the address. Free some space on this device, then tap Try again."
            return
        }
        var updated = host
        updated.address = trimmedAddress
        onRetry(updated)
    }
}
