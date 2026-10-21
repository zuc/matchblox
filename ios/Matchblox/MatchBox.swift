import SwiftUI

/// What the box shows (design/0009 §6).
enum BoxPose: Equatable {
    /// The sleeve and the striker in lines, without fill: the viewfinder.
    case outline
    /// The solid box, shut.
    case shut
    /// The opening of §5 from this date. It ends on the standing, burning match.
    case open(Date)
    /// The box stays shut. The label shows the failure.
    case failed
}

/// The site's theme-switch box, front-on: the sleeve, the striker along its
/// top, the drawer that slides out to the right, and the match. It is taller
/// than the site's, as on the host, so the sleeve holds a square window.
/// The label sits in that window.
struct MatchBox: View {
    let pose: BoxPose
    let label: AnyView?

    init(pose: BoxPose, label: AnyView? = nil) {
        self.pose = pose
        self.label = label
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// The moment the head reaches the end of the striker, from the start of the opening.
    static let strikeAt: TimeInterval = (Step.open + Step.out + Step.strike) / 1000
    /// The moment the match stands, from the start of the opening.
    static let stoodAt: TimeInterval = strikeAt + Step.stand / 1000
    /// The box's width over its height.
    static let aspectRatio = Geom.space.width / Geom.space.height

    /// The window of the sleeve, in a MatchBox view of this size.
    static func window(in size: CGSize) -> CGRect {
        Fit(size).rect(Geom.window)
    }

    var body: some View {
        GeometryReader { geo in
            let fit = Fit(geo.size)
            switch pose {
            case .open(let start) where !reduceMotion:
                TimelineView(.animation) { ctx in
                    layers(fit, Frame(at: ctx.date.timeIntervalSince(start) * 1000))
                }
            case .open:
                layers(fit, Frame(at: .infinity, still: true))
            case .outline, .shut, .failed:
                layers(fit, nil)
            }
        }
        .aspectRatio(Self.aspectRatio, contentMode: .fit)
    }

    /// The box at a moment of the opening; a nil frame is the shut box.
    private func layers(_ fit: Fit, _ frame: Frame?) -> some View {
        let window = fit.rect(Geom.window)
        return ZStack(alignment: .topLeading) {
            Canvas { ctx, _ in
                fit.apply(to: &ctx)
                if pose == .outline {
                    drawOutline(&ctx, fit)
                } else {
                    drawBox(&ctx, fit, frame)
                }
            }
            if let label {
                label
                    .frame(width: window.width, height: window.height)
                    .clipped()
                    .position(x: window.midX, y: window.midY)
            }
            if let frame {
                Canvas { ctx, _ in
                    fit.apply(to: &ctx)
                    drawMatch(&ctx, frame)
                }
            }
        }
    }

    private func drawOutline(_ ctx: inout GraphicsContext, _ fit: Fit) {
        ctx.stroke(Path(roundedRect: Geom.sleeve, cornerRadius: 2), with: .color(Theme.muted), lineWidth: 1.5 / fit.scale)
        drawGrit(&ctx, Theme.hair)
    }

    private func drawBox(_ ctx: inout GraphicsContext, _ fit: Fit, _ frame: Frame?) {
        let drawer = frame?.drawer ?? 0
        if let frame, frame.halo > 0 {
            var glow = ctx
            glow.opacity = frame.halo
            let head = Geom.upright.apply(CGPoint(x: 38, y: 0))
            let centre = CGPoint(x: head.x, y: head.y - 10)
            glow.fill(
                Path(ellipseIn: CGRect(x: centre.x - 16, y: centre.y - 16, width: 32, height: 32)),
                with: .radialGradient(
                    Gradient(colors: [Theme.flame.opacity(0.45), Theme.flame.opacity(0)]),
                    center: centre, startRadius: 0, endRadius: 16))
        }
        if drawer > 0 {
            let tray = Path(roundedRect: Geom.drawer.offsetBy(dx: drawer * Geom.drawerOut, dy: 0), cornerRadius: 1)
            ctx.fill(tray, with: .color(Theme.markTray))
            ctx.stroke(tray, with: .color(Theme.markLeft), lineWidth: 1.5 / fit.scale)
        }
        let sleeve = Path(roundedRect: Geom.sleeve, cornerRadius: 2)
        ctx.fill(sleeve, with: .color(Theme.markRight))
        ctx.stroke(sleeve, with: .color(Theme.markLeft), lineWidth: 2 / fit.scale)
        drawGrit(&ctx, Theme.markStriker)
    }

    /// The striker: the site's grit pattern, strips 1.4 wide every 3 at 45°.
    private func drawGrit(_ ctx: inout GraphicsContext, _ ink: Color) {
        ctx.drawLayer { layer in
            let s = Geom.striker
            layer.clip(to: Path(s))
            var grit = Path()
            let step = 3 * 2.squareRoot()
            var x = s.minX - s.height
            while x < s.maxX + s.height {
                grit.move(to: CGPoint(x: x, y: s.maxY))
                grit.addLine(to: CGPoint(x: x + s.height, y: s.minY))
                x += step
            }
            layer.stroke(grit, with: .color(ink), lineWidth: 1.4)
        }
    }

    private func drawMatch(_ ctx: inout GraphicsContext, _ frame: Frame) {
        if frame.spark.opacity > 0 {
            var spark = ctx
            spark.opacity = frame.spark.opacity
            spark.translateBy(x: Geom.spark.x, y: Geom.spark.y)
            spark.scaleBy(x: frame.spark.scale, y: frame.spark.scale)
            var rays = Path()
            for (a, b) in [((0.0, -5.0), (0.0, 5.0)), ((-5, 0), (5, 0)), ((-3.5, -3.5), (3.5, 3.5)), ((-3.5, 3.5), (3.5, -3.5))] {
                rays.move(to: CGPoint(x: a.0, y: a.1))
                rays.addLine(to: CGPoint(x: b.0, y: b.1))
            }
            spark.stroke(rays, with: .color(Theme.flameCore), style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
        }
        guard frame.match.opacity > 0 else { return }
        var m = ctx
        m.opacity = frame.match.opacity
        m.translateBy(x: frame.match.pose.x, y: frame.match.pose.y)
        m.rotate(by: .degrees(frame.match.pose.angle))
        var stick = Path()
        stick.move(to: .zero)
        stick.addLine(to: CGPoint(x: 35, y: 0))
        m.stroke(stick, with: .color(Theme.markStick), style: StrokeStyle(lineWidth: 3, lineCap: .round))
        m.fill(Path(ellipseIn: CGRect(x: 38 - 3.6, y: -3.6, width: 7.2, height: 7.2)), with: .color(Theme.matchHead))
        guard frame.flame > 0 else { return }
        // The flame grows from its root, the left end of the fire.
        m.translateBy(x: 40.5, y: 0)
        m.scaleBy(x: frame.flame, y: frame.flame)
        m.translateBy(x: -40.5, y: 0)
        for (tongue, ink, period) in [(Geom.fire, Theme.flame, 1600.0), (Geom.amber, Theme.flameAmber, 1600), (Geom.core, Theme.flameCore, 1100)] {
            let k = Frame.flicker(frame.lit, period)
            let root = tongue.boundingRect.minX
            var f = m
            f.translateBy(x: root, y: 0)
            f.scaleBy(x: k.width, y: k.height)
            f.translateBy(x: -root, y: 0)
            f.fill(tongue, with: .color(ink))
        }
    }
}

/// The steps of the opening in ms, the site's timings (design/0009 §5).
private enum Step {
    static let open = 260.0
    static let out = 280.0
    static let strike = 150.0
    static let stand = 320.0
    static let shut = 260.0 // the drawer, while the match stands
    static let flame = 240.0
    static let spark = 260.0
}

/// The site's `#match` svg (viewBox 0 0 100 66), made taller so the sleeve
/// holds a square window under the striker. The space starts at x -8: the
/// tail of the match goes left of the sleeve in the strike. It starts at
/// y 16: nothing draws higher.
private enum Geom {
    static let space = CGRect(x: -8, y: 16, width: 108, height: 76)
    static let sleeve = CGRect(x: 8, y: 30, width: 56, height: 50)
    static let striker = CGRect(x: 9, y: 31, width: 54, height: 5)
    static let window = CGRect(x: 18, y: 38, width: 36, height: 36)
    static let drawer = CGRect(x: 10, y: 56, width: 52, height: 21)
    static let drawerOut: CGFloat = 30
    static let spark = CGPoint(x: 64, y: 31)

    // The poses of the match: the tail at the origin, the head at +x.
    static let boxed = MatchPose(x: 54, y: 66, angle: 0)
    static let strikeFrom = MatchPose(x: -4, y: 41, angle: -15)
    static let strikeTo = MatchPose(x: 27, y: 41, angle: -15)
    static let upright = MatchPose(x: 84, y: 86, angle: -90)

    static let fire = tongue(40.5, 44, 50, 57, 5.5, 5)
    static let amber = tongue(41, 44, 48.5, 53.5, 4, 3.7)
    static let core = tongue(41.5, 44, 47, 50, 2.6, 2.4)

    /// A flame along +x from root to tip, as the site's fire and core paths.
    private static func tongue(_ root: CGFloat, _ c1: CGFloat, _ c2: CGFloat, _ tip: CGFloat, _ h1: CGFloat, _ h2: CGFloat) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: root, y: 0))
        p.addCurve(to: CGPoint(x: tip, y: 0), control1: CGPoint(x: c1, y: -h1), control2: CGPoint(x: c2, y: -h2))
        p.addCurve(to: CGPoint(x: root, y: 0), control1: CGPoint(x: c2, y: h2), control2: CGPoint(x: c1, y: h1))
        p.closeSubpath()
        return p
    }
}

private struct MatchPose {
    var x, y, angle: CGFloat

    func lerp(_ to: MatchPose, _ k: CGFloat) -> MatchPose {
        MatchPose(x: x + (to.x - x) * k, y: y + (to.y - y) * k, angle: angle + (to.angle - angle) * k)
    }

    func apply(_ p: CGPoint) -> CGPoint {
        let a = angle * .pi / 180
        return CGPoint(x: x + p.x * cos(a) - p.y * sin(a), y: y + p.x * sin(a) + p.y * cos(a))
    }
}

/// The space of Geom, fitted and centred in a view of a size.
private struct Fit {
    let scale: CGFloat
    let origin: CGPoint

    init(_ size: CGSize) {
        let s = Geom.space
        scale = max(min(size.width / s.width, size.height / s.height), .ulpOfOne)
        origin = CGPoint(x: (size.width - s.width * scale) / 2 - s.minX * scale, y: (size.height - s.height * scale) / 2 - s.minY * scale)
    }

    func rect(_ r: CGRect) -> CGRect {
        CGRect(x: origin.x + r.minX * scale, y: origin.y + r.minY * scale, width: r.width * scale, height: r.height * scale)
    }

    func apply(to ctx: inout GraphicsContext) {
        ctx.translateBy(x: origin.x, y: origin.y)
        ctx.scaleBy(x: scale, y: scale)
    }
}

/// The opening at a moment, as www/app.js plays it.
private struct Frame {
    var drawer: CGFloat = 0
    var match = (pose: Geom.boxed, opacity: CGFloat(0))
    var spark = (opacity: CGFloat(0), scale: CGFloat(1))
    var flame: CGFloat = 0
    var halo: CGFloat = 0
    var lit: Double = 0 // ms since the strike, for the flicker

    init(at t: Double, still: Bool = false) {
        let openEnd = Step.open, outEnd = openEnd + Step.out, strikeEnd = outEnd + Step.strike, standEnd = strikeEnd + Step.stand
        switch t {
        case ..<openEnd:
            drawer = ease(t / Step.open)
        case ..<strikeEnd:
            drawer = 1
        default:
            drawer = 1 - ease((t - strikeEnd) / Step.shut)
        }
        switch t {
        case ..<openEnd:
            break
        case ..<outEnd:
            // The match shows in the drawer in the first 20%, then goes to the striker.
            let p = ease((t - openEnd) / Step.out)
            match = p < 0.2 ? (Geom.boxed, p / 0.2) : (Geom.boxed.lerp(Geom.strikeFrom, (p - 0.2) / 0.8), 1)
        case ..<strikeEnd:
            match = (Geom.strikeFrom.lerp(Geom.strikeTo, easeIn((t - outEnd) / Step.strike)), 1)
        case ..<standEnd:
            match = (Geom.strikeTo.lerp(Geom.upright, ease((t - strikeEnd) / Step.stand)), 1)
        default:
            match = (Geom.upright, 1)
        }
        guard t >= strikeEnd else { return }
        if still {
            // Reduce Motion: the settled flame, with no spark and no flicker.
            flame = 1
            halo = 1
            return
        }
        lit = t - strikeEnd
        let u = lit / Step.spark
        if u < 1 {
            spark = u < 0.3 ? (u / 0.3, 0.4 + 0.8 * u / 0.3) : (1 - (u - 0.3) / 0.7, 1.2 + 0.4 * (u - 0.3) / 0.7)
        }
        let p = ease(lit / Step.flame)
        flame = p < 0.6 ? 1.25 * p / 0.6 : 1.25 - 0.25 * (p - 0.6) / 0.4
        let pulse = (lit / 1600).truncatingRemainder(dividingBy: 1)
        halo = easeOut(lit / Step.flame) * (pulse < 0.5 ? 1 - 0.25 * easeInOut(pulse / 0.5) : 0.75 + 0.25 * easeInOut((pulse - 0.5) / 0.5))
    }

    /// The site's flicker: the scale of a tongue at ms into the burn.
    static func flicker(_ ms: Double, _ period: Double) -> CGSize {
        let keys: [CGSize] = [CGSize(width: 1, height: 1), CGSize(width: 1.06, height: 0.92), CGSize(width: 0.95, height: 1.06), CGSize(width: 1.04, height: 0.96), CGSize(width: 1, height: 1)]
        let f = (ms / period).truncatingRemainder(dividingBy: 1) * 4
        let i = min(Int(f), 3)
        let k = easeInOut(f - Double(i))
        return CGSize(width: keys[i].width + (keys[i + 1].width - keys[i].width) * k, height: keys[i].height + (keys[i + 1].height - keys[i].height) * k)
    }
}

// The site's easings.
private let ease = CubicBezier(0.2, 0.7, 0.2, 1)
private let easeIn = CubicBezier(0.55, 0, 1, 0.45)
private let easeOut = CubicBezier(0, 0, 0.58, 1)
private let easeInOut = CubicBezier(0.42, 0, 0.58, 1)

/// A CSS cubic-bezier timing function.
private struct CubicBezier: Sendable {
    let x1, y1, x2, y2: Double

    init(_ x1: Double, _ y1: Double, _ x2: Double, _ y2: Double) {
        (self.x1, self.y1, self.x2, self.y2) = (x1, y1, x2, y2)
    }

    func callAsFunction(_ x: Double) -> Double {
        let x = min(max(x, 0), 1)
        var lo = 0.0, hi = 1.0
        for _ in 0..<24 {
            let mid = (lo + hi) / 2
            if Self.curve(mid, x1, x2) < x { lo = mid } else { hi = mid }
        }
        return Self.curve((lo + hi) / 2, y1, y2)
    }

    private static func curve(_ t: Double, _ a: Double, _ b: Double) -> Double {
        let u = 1 - t
        return 3 * u * u * t * a + 3 * u * t * t * b + t * t * t
    }
}

#Preview("Outline") {
    MatchBox(pose: .outline, label: AnyView(Color.black.opacity(0.4)))
        .padding(24)
        .background(Theme.bg)
}

#Preview("Shut") {
    MatchBox(pose: .shut, label: AnyView(ZStack { Theme.fg; Text("devbox").foregroundStyle(Theme.bg) }))
        .padding(24)
        .background(Theme.bg)
}

#Preview("Open") {
    @Previewable @State var start = Date()
    MatchBox(pose: .open(start), label: AnyView(ZStack { Theme.fg; Text("devbox").foregroundStyle(Theme.bg) }))
        .padding(24)
        .background(Theme.bg)
        .onTapGesture { start = Date() }
}

#Preview("Failed") {
    MatchBox(pose: .failed, label: AnyView(ZStack { Theme.fg; Text("devbox").foregroundStyle(Theme.faint) }))
        .padding(24)
        .background(Theme.bg)
}
