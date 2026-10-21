# matchblox for iPhone and iPad

This app opens the matchblox console on your iPhone or iPad.
It pairs with your host by QR code. Then it connects over SSH.

## Build

You need a Mac with Xcode 16 and Homebrew. The app needs iOS 18.0 or later.

1. `make install` installs XcodeGen.
2. `make gen` makes `Matchblox.xcodeproj` from `project.yml`.
3. `make build` builds the app for the simulator.

To run the app on your iPhone or iPad, open `Matchblox.xcodeproj` in Xcode.
Select the Matchblox target. Set your team under Signing & Capabilities.
The first build asks you to trust the build plugin of SwiftTerm. Tap
Trust & Enable. Xcode asks only once.

If the build says that the Metal Toolchain is missing, run
`xcodebuild -downloadComponent MetalToolchain`. SwiftTerm needs it.

The app has two extensions: MatchbloxIsland (the Live Activity) and
MatchbloxBuzz (the buzz). Set your team on all three targets. The app needs
these capabilities: Push Notifications, App Attest, App Groups
(`group.com.blox.matchblox`) and Keychain Sharing
(`com.blox.matchblox.shared`). The extensions carry the App Group, and the
buzz also the keychain group.

If Xcode says that the bundle identifier is not available, give your
build its own identifier. Do not change `project.yml`. The app, both
extensions, the App Group and the keychain group all derive from the one
setting `MATCHBLOX_BUNDLE_ID`, so one override is enough:

```sh
xcodebuild -scheme Matchblox -destination 'id=<device id>' \
  -allowProvisioningUpdates DEVELOPMENT_TEAM=<team> \
  MATCHBLOX_BUNDLE_ID=com.blox.matchblox.<you> build
```

`xcrun devicectl list devices` shows the device id.

`make icon` makes the app icon from `AppIcon.svg`, the mark of DESIGN.md §3.
It needs `rsvg-convert` (`brew install librsvg`).

`make test` runs the tests of the MatchbloxKit package.

## CI

The workflow `.github/workflows/ios.yml` runs on a macOS runner.
It runs `swift test` on MatchbloxKit. Then it runs `make build`.
It starts when you change `ios/`, `testdata/pair/` or the workflow.

## Check on a device

CI has no Secure Enclave, no camera and no Face ID. Do these checks on an
iPhone before a release. `ws-1` is the host.

1. On the host, run `matchblox pair`. Scan the code with the Camera, then
   tap Pair. The app opens the console. The host prints
   `Paired <name> (<fingerprint>)`.
2. `matchblox devices` on the host lists the iPhone.
3. Turn the iPhone on its side. The console takes the landscape layout.
   Open the keyboard. The console and the key row fit above it.
4. Attach a laptop to the same session. The size follows the device that
   you used last.
5. Quit and open the app again. You see one Face ID, then the console.
   With one host, there is no hosts list.
6. Change the address of the host to a wrong one. You see
   `Can't reach ws-1.` Type the right address. You see
   `Connecting to ws-1`, then the console.
7. Scan the same code again. You see
   "This code was used or expired. Run matchblox pair on ws-1 again."
8. Open the console and cancel Face ID. You see "The key is still locked.
   Tap Try again, then use Face ID or the passcode." Tap Try again. You
   do not see "Can't reach".
9. Fail Face ID, then type the passcode slowly, for more than 10 s. The
   console still opens.
10. Signing after the unlock does not show a second Face ID.
11. Make `matchblox console` stop at once on the host. The app tries 3
    times with no new Face ID. Then it says
    "matchblox console on ws-1 stops at once. …". Fix the host, then tap
    Try again. The console opens with no Face ID.
12. Quit the console on the host. The app says
    "matchblox console on ws-1 ended." Tap Open again. The console opens.
13. Put the app in the background and open it again. You see one Face ID,
    then the console.
14. Change the network while the console is open (for example, Wi-Fi to
    mobile data). The app has no keepalive yet: write down what you see.
15. Pair two hosts. Swipe to remove one. The app says how to remove its
    key on the host.
