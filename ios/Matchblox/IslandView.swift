import MatchbloxKit
import SwiftUI

/// The Island setting (design 0010 §4.3). Show changes only the phone; Follow and Buzz
/// register again with the hosts concerned, with one Face ID.
struct IslandView: View {
    let store: HostStore
    let onBack: () -> Void

    @State private var hosts: [Host] = []
    @State private var prefs = IslandSettings.prefs
    @State private var unreachable: [String] = []
    @State private var pending: Set<String> = []
    @State private var task: Task<Void, Never>?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Island")
                .font(.title3)
            ForEach(unreachable, id: \.self) { label in
                Text(Opening.notReachable(host: label))
                    .foregroundStyle(Theme.muted)
            }
            Form {
                Section("Follow") {
                    Picker("Follow", selection: follow) {
                        Text("None").tag("")
                        ForEach(hosts) { host in
                            Text(host.label).tag(host.fingerprint)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                .listRowBackground(Theme.bg)
                Section("Show") {
                    Picker("Show", selection: $prefs.show) {
                        Text("Sessions").tag(Show.sessions)
                        Text("Usage").tag(Show.usage)
                        Text("Auto (Usage while the stoker runs)").tag(Show.auto)
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                .listRowBackground(Theme.bg)
                Section("Buzz") {
                    ForEach(hosts) { host in
                        Toggle(host.label, isOn: buzz(host))
                    }
                }
                .listRowBackground(Theme.bg)
            }
            .scrollContentBackground(.hidden)
            Button("Hosts", action: onBack)
                .foregroundStyle(Theme.muted)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(24)
        .onAppear {
            hosts = store.all()
            prefs = IslandSettings.prefs
        }
        .onChange(of: prefs) { old, new in
            IslandSettings.prefs = new
            let concerned = IslandPrefs.concerned(old, new, hosts: hosts.map(\.fingerprint))
            guard !concerned.isEmpty else { return }
            pending.formUnion(concerned)
            schedule()
        }
    }

    private var follow: Binding<String> {
        Binding(
            get: { prefs.follow.flatMap { fp in hosts.contains { $0.fingerprint == fp } ? fp : nil } ?? "" },
            set: { prefs.choice = $0.isEmpty ? FollowChoice.none : .host($0) }
        )
    }

    private func buzz(_ host: Host) -> Binding<Bool> {
        Binding(
            get: { prefs.buzzOn(host.fingerprint) },
            set: { prefs.buzz[host.fingerprint] = $0 }
        )
    }

    /// A short pause gathers quick changes into one Face ID.
    private func schedule() {
        task?.cancel()
        task = Task {
            try? await Task.sleep(for: .milliseconds(800))
            guard !Task.isCancelled else { return }
            let due = hosts.filter { pending.contains($0.fingerprint) }
            pending = []
            let missed = await Registration.afterChange(hosts: due)
            let labels = Set(due.map(\.label))
            unreachable = unreachable.filter { !labels.contains($0) } + missed.map(\.label)
        }
    }
}
