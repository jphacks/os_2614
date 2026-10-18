import {
  AbsoluteFill,
  Easing,
  interpolate,
  OffthreadVideo,
  Sequence,
  spring,
  staticFile,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";

export const FPS = 30;
export const TOTAL_FRAMES = 45 * FPS;

const C = {
  bg: "#F3F1EE",
  ink: "#3F3B38",
  sub: "#7A746F",
  accent: "#9CC3EA",
  cloud: "#D9D9D9",
};
const FONT = '"Hiragino Sans", "Hiragino Kaku Gothic ProN", "Noto Sans JP", sans-serif';

type Step = { title: string; sub: string };
const STEPS: Step[] = [
  { title: "Bluesky でログイン", sub: "アカウントは Bluesky のものをそのまま使う" },
  { title: "プロフィールを編集", sub: "名前と自己紹介は自分の PDS に保存される" },
  { title: "すれ違った人が「ちかく」に", sub: "すれ違いが泡になって並ぶ" },
  { title: "プロフィールを見て友達申請", sub: "申請は Bluesky でのフォロー" },
  { title: "友達と会うとパーティーに", sub: "近くにいる友達と自動でまとまる" },
  { title: "フィードに投稿", sub: "すれ違った人や友達の投稿が届く" },
  { title: "ホームのグラフ", sub: "人・グループ・パーティーのつながりを一目で" },
];

// 素材は実機の screenrecord を 30fps / 720x1600 にしたもの（public/clips）。
// from / to は素材の秒、rate は再生速度。
type Cut = { clip: string; from: number; to: number; rate: number; step: number };
const CUTS: Cut[] = [
  { clip: "s1", from: 1.0, to: 6.6, rate: 1.6, step: 0 }, // 起動 →「Blueskyで続ける」
  { clip: "s1", from: 32.8, to: 34.2, rate: 1.0, step: 0 }, // 認可画面
  { clip: "s1", from: 55.2, to: 58.6, rate: 1.4, step: 0 }, // ログイン直後の「ちかく」
  { clip: "s2", from: 2.3, to: 12.3, rate: 2.5, step: 1 }, // 編集
  { clip: "s3", from: 0.2, to: 1.4, rate: 1.0, step: 1 }, // 保存後
  { clip: "s3", from: 3.5, to: 7.5, rate: 1.6, step: 2 }, // すれ違いの泡
  { clip: "s3", from: 35.8, to: 38.6, rate: 1.4, step: 3 }, // applemango を選ぶ
  { clip: "s3", from: 45.0, to: 47.6, rate: 1.3, step: 3 }, // プロフィール
  { clip: "s3", from: 57.6, to: 60.8, rate: 1.6, step: 3 }, // フォロー
  { clip: "s4", from: 0.8, to: 6.0, rate: 1.6, step: 4 }, // パーティー
  { clip: "s4", from: 13.5, to: 16.5, rate: 2.4, step: 4 }, // メンバーの投稿
  { clip: "s5", from: 3.2, to: 5.6, rate: 1.5, step: 5 }, // フィード
  { clip: "s5", from: 12.8, to: 15.0, rate: 1.3, step: 5 }, // 投稿画面を開く
  { clip: "s5", from: 44.0, to: 62.0, rate: 3.6, step: 5 }, // 公開を選んで入力・投稿
  { clip: "s5b", from: 2.6, to: 5.4, rate: 1.4, step: 5 }, // フィードに出る
  { clip: "s6", from: 3.6, to: 8.8, rate: 1.1, step: 6 }, // ちかくの泡 → グラフ
];

const INTRO = Math.round(1.5 * FPS);
const cutFrames = (c: Cut) => Math.round(((c.to - c.from) / c.rate) * FPS);
const CUT_START: number[] = [];
CUTS.reduce((acc, c, i) => {
  CUT_START[i] = acc;
  return acc + cutFrames(c);
}, INTRO);
const BODY_END = CUT_START[CUTS.length - 1] + cutFrames(CUTS[CUTS.length - 1]);
const OUTRO = TOTAL_FRAMES - BODY_END;

// step ごとの区間（キャプションの切り替えに使う）
const STEP_RANGE = STEPS.map((_, s) => {
  const idx = CUTS.map((c, i) => (c.step === s ? i : -1)).filter((i) => i >= 0);
  const first = idx[0];
  const last = idx[idx.length - 1];
  return { start: CUT_START[first], end: CUT_START[last] + cutFrames(CUTS[last]) };
});

const Cloud = ({ size, color = C.cloud }: { size: number; color?: string }) => (
  <svg width={size} height={size} viewBox="0 0 260 260">
    <circle cx="117" cy="159" r="59" fill={color} />
    <circle cx="197" cy="140" r="59" fill={color} />
    <circle cx="64" cy="126" r="59" fill={color} />
    <circle cx="148" cy="100" r="59" fill={color} />
    <circle cx="96.5" cy="121.5" r="15.5" fill="white" />
    <circle cx="169.5" cy="121.5" r="15.5" fill="white" />
  </svg>
);

// 背景にゆっくり漂う泡
const Background = () => {
  const f = useCurrentFrame();
  const bubbles = [
    { x: 140, y: 820, r: 120, c: "#E4ECF5", s: 0.6 },
    { x: 1700, y: 180, r: 160, c: "#E9E4DE", s: 0.4 },
    { x: 1780, y: 900, r: 90, c: "#E4ECF5", s: 0.8 },
    { x: 420, y: 120, r: 70, c: "#ECE7E1", s: 1.0 },
    { x: 1120, y: 1010, r: 110, c: "#ECE7E1", s: 0.5 },
  ];
  return (
    <AbsoluteFill style={{ background: C.bg }}>
      {bubbles.map((b, i) => (
        <div
          key={i}
          style={{
            position: "absolute",
            left: b.x - b.r,
            top: b.y - b.r + Math.sin((f / FPS) * b.s + i) * 18,
            width: b.r * 2,
            height: b.r * 2,
            borderRadius: "50%",
            background: b.c,
          }}
        />
      ))}
    </AbsoluteFill>
  );
};

const PHONE_H = 940;
const PHONE_W = (PHONE_H * 720) / 1600;

const Phone = () => {
  const f = useCurrentFrame();
  const { fps } = useVideoConfig();
  const enter = spring({ frame: f - INTRO + 2, fps, config: { damping: 18 } });
  const exit = interpolate(f, [BODY_END - 10, BODY_END + 2], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.in(Easing.cubic),
  });
  const bezel = 14;
  return (
    <div
      style={{
        position: "absolute",
        left: 1180 - PHONE_W / 2 - bezel,
        top: (1080 - PHONE_H) / 2 - bezel,
        width: PHONE_W + bezel * 2,
        height: PHONE_H + bezel * 2,
        borderRadius: 58,
        background: "#1E1C1B",
        padding: bezel,
        boxShadow: "0 40px 80px rgba(60,50,40,0.25)",
        transform: `translateY(${(1 - enter) * 1100 + exit * 1100}px) rotate(${(1 - enter) * 6}deg)`,
      }}
    >
      <div style={{ width: PHONE_W, height: PHONE_H, borderRadius: 44, overflow: "hidden", position: "relative", background: "#fff" }}>
        {CUTS.map((c, i) => (
          <Sequence key={i} from={CUT_START[i]} durationInFrames={cutFrames(c)} layout="none">
            <OffthreadVideo
              src={staticFile(`clips/${c.clip}.mp4`)}
              trimBefore={Math.round(c.from * FPS)}
              playbackRate={c.rate}
              muted
              style={{ width: PHONE_W, height: PHONE_H, position: "absolute", inset: 0 }}
            />
          </Sequence>
        ))}
      </div>
    </div>
  );
};

const Caption = ({ step, index }: { step: Step; index: number }) => {
  const f = useCurrentFrame(); // Sequence 内の相対フレーム
  const { fps, durationInFrames } = useVideoConfig();
  const inP = spring({ frame: f, fps, config: { damping: 16 } });
  const outP = interpolate(f, [durationInFrames - 8, durationInFrames], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const subIn = spring({ frame: f - 6, fps, config: { damping: 18 } });
  return (
    <div style={{ position: "absolute", left: 150, top: 380, width: 660, fontFamily: FONT, color: C.ink }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 16,
          opacity: inP * (1 - outP),
          transform: `translateX(${(1 - inP) * -40}px)`,
        }}
      >
        <div
          style={{
            width: 54,
            height: 54,
            borderRadius: 27,
            background: C.ink,
            color: "#fff",
            fontSize: 28,
            fontWeight: 700,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          {index + 1}
        </div>
        <div style={{ fontSize: 22, letterSpacing: 4, color: C.sub, fontWeight: 600 }}>STEP {index + 1} / {STEPS.length}</div>
      </div>
      <div
        style={{
          marginTop: 28,
          fontSize: 62,
          fontWeight: 800,
          lineHeight: 1.25,
          opacity: inP * (1 - outP),
          transform: `translateY(${(1 - inP) * 30 - outP * 20}px)`,
        }}
      >
        {step.title}
      </div>
      <div
        style={{
          marginTop: 22,
          fontSize: 30,
          color: C.sub,
          fontWeight: 500,
          opacity: subIn * (1 - outP),
          transform: `translateY(${(1 - subIn) * 20}px)`,
        }}
      >
        {step.sub}
      </div>
    </div>
  );
};

const Progress = () => {
  const f = useCurrentFrame();
  const visible = interpolate(f, [INTRO, INTRO + 10, BODY_END - 6, BODY_END], [0, 1, 1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  return (
    <div style={{ position: "absolute", left: 150, top: 760, width: 560, opacity: visible }}>
      <div style={{ display: "flex", gap: 10 }}>
        {STEP_RANGE.map((r, i) => {
          const fill = interpolate(f, [r.start, r.end], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
          return (
            <div key={i} style={{ flex: 1, height: 6, borderRadius: 3, background: "#DDD7D0", overflow: "hidden" }}>
              <div style={{ width: `${fill * 100}%`, height: "100%", background: C.ink }} />
            </div>
          );
        })}
      </div>
    </div>
  );
};

const Title = ({ outro }: { outro?: boolean }) => {
  // outro は端末が下へ抜けきってから出す
  const f = useCurrentFrame() - (outro ? 6 : 0);
  const { fps, durationInFrames } = useVideoConfig();
  const s = spring({ frame: f, fps, config: { damping: 12 } });
  const fade = outro
    ? 1
    : interpolate(f, [durationInFrames - 8, durationInFrames], [1, 0], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  const tag = spring({ frame: f - 10, fps, config: { damping: 18 } });
  return (
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", fontFamily: FONT, opacity: fade }}>
      <div style={{ transform: `scale(${0.6 + s * 0.4}) translateY(${Math.sin(f / 8) * 4}px)` }}>
        <Cloud size={300} />
      </div>
      <div style={{ fontSize: 120, fontWeight: 800, letterSpacing: 18, color: C.ink, marginTop: -20, transform: `scale(${0.8 + s * 0.2})`, opacity: s }}>
        CHAIN
      </div>
      <div style={{ fontSize: 36, color: C.sub, marginTop: 12, opacity: tag, transform: `translateY(${(1 - tag) * 16}px)` }}>
        {outro ? "Bluesky で、すれ違いからつながる。" : "すれ違いから、つながる。"}
      </div>
    </AbsoluteFill>
  );
};

export const Demo = () => (
  <AbsoluteFill style={{ fontFamily: FONT }}>
    <Background />
    <Sequence durationInFrames={INTRO}>
      <Title />
    </Sequence>
    {STEPS.map((step, i) => (
      <Sequence key={i} from={STEP_RANGE[i].start} durationInFrames={STEP_RANGE[i].end - STEP_RANGE[i].start}>
        <Caption step={step} index={i} />
      </Sequence>
    ))}
    <Progress />
    <Phone />
    <Sequence from={BODY_END} durationInFrames={OUTRO}>
      <Title outro />
    </Sequence>
  </AbsoluteFill>
);
