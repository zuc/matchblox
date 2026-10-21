// swift-tools-version:5.10
import PackageDescription

let package = Package(
    name: "MatchbloxKit",
    platforms: [.iOS("18.0"), .macOS("14.0")],
    products: [
        .library(name: "MatchbloxKit", targets: ["MatchbloxKit"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-nio.git", exact: "2.103.0"),
        .package(url: "https://github.com/apple/swift-nio-ssh", from: "0.15.0"),
        .package(url: "https://github.com/apple/swift-nio-transport-services", from: "1.28.0"),
    ],
    targets: [
        .target(
            name: "MatchbloxKit",
            dependencies: [
                .product(name: "NIOCore", package: "swift-nio"),
                .product(name: "NIOSSH", package: "swift-nio-ssh"),
                .product(name: "NIOTransportServices", package: "swift-nio-transport-services"),
            ],
            swiftSettings: [.enableUpcomingFeature("StrictConcurrency")]
        ),
        .testTarget(
            name: "MatchbloxKitTests",
            dependencies: [
                "MatchbloxKit",
                .product(name: "NIOCore", package: "swift-nio"),
                .product(name: "NIOSSH", package: "swift-nio-ssh"),
                .product(name: "NIOTransportServices", package: "swift-nio-transport-services"),
            ]
        ),
    ]
)
