import ActivityKit
import SwiftUI
import WidgetKit

struct MatchbloxIslandActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: IslandAttributes.self) { _ in
            EmptyView()
        } dynamicIsland: { _ in
            DynamicIsland {
                DynamicIslandExpandedRegion(.center) { EmptyView() }
            } compactLeading: {
                EmptyView()
            } compactTrailing: {
                EmptyView()
            } minimal: {
                EmptyView()
            }
        }
    }
}

@main
struct MatchbloxIslandBundle: WidgetBundle {
    var body: some Widget {
        MatchbloxIslandActivity()
    }
}
