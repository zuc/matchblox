import MatchbloxKit
import SwiftUI

struct HostsView: View {
    let store: HostStore
    let onOpen: (Host) -> Void
    let onPair: () -> Void
    let onIsland: () -> Void
    let onNoHosts: (String) -> Void

    @State private var hosts: [Host] = []
    @State private var removed: String?
    @State private var problem: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Hosts")
                .font(.title3)
            if let removed {
                Text(Opening.removedNotice(removed))
                    .foregroundStyle(Theme.muted)
            }
            if let problem {
                Text(problem)
                    .foregroundStyle(Theme.neg)
            }
            List {
                ForEach(hosts) { host in
                    Button { onOpen(host) } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(host.label)
                            Text(host.address)
                                .font(.footnote)
                                .foregroundStyle(Theme.faint)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .contextMenu {
                        Button("Island", action: onIsland)
                    }
                    .listRowBackground(Theme.bg)
                    .listRowSeparatorTint(Theme.hair)
                }
                .onDelete(perform: remove)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            HStack(spacing: 24) {
                Button("+ Pair a host", action: onPair)
                    .foregroundStyle(Theme.accent)
                Button("Island", action: onIsland)
                    .foregroundStyle(Theme.muted)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(24)
        .onAppear { hosts = store.all() }
    }

    private func remove(at offsets: IndexSet) {
        let gone = offsets.map { hosts[$0] }
        do {
            for host in gone {
                try store.remove(fingerprint: host.fingerprint)
            }
        } catch {
            problem = "This device could not save the list. Free some space on this device, then try again."
            hosts = store.all()
            return
        }
        for host in gone {
            try? DeviceKey.remove(host: host.fingerprint)
        }
        problem = nil
        let labels = gone.map(\.label).joined(separator: ", ")
        removed = labels
        hosts = store.all()
        if hosts.isEmpty { onNoHosts(Opening.removedNotice(labels)) }
    }
}
