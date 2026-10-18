import { Composition } from "remotion";
import { Demo, FPS, TOTAL_FRAMES } from "./Demo";

export const RemotionRoot = () => (
  <Composition
    id="Demo"
    component={Demo}
    durationInFrames={TOTAL_FRAMES}
    fps={FPS}
    width={1920}
    height={1080}
  />
);
