import ActivityKit
import MatchbloxKit

/// The one Live Activity: a host's island. Shared by the app (push to start, stray
/// activities) and the widget (the faces).
struct IslandAttributes: ActivityAttributes {
    typealias ContentState = IslandState
    /// The followed host's 8 characters of its key fingerprint.
    let host: String
}
