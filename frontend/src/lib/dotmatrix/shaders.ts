/**
 * GLSL ES 3.00 shaders for the two-pass procedural dot-matrix wait & reveal renderer.
 *
 * Pass 0 (startup, once): half-res static 2-octave noise texture for Noise dissolve.
 * Pass 1 (per active box): 1 texel per dot field texture evaluating one of 9 loading patterns.
 * Pass 2 (per active box): full-res rounded box drawing dots, vignette, dark-only halos &
 *         counter-phase edge glow, plus all 9 reveals with shared zoom, mip blur, edge/front
 *         glass bend, movement-only dispersion, neutral white front rim, and 3-reveal shockwave.
 */

export const FULLSCREEN_VERT = `#version 300 es
precision highp float;

const vec2 POS[3] = vec2[3](
  vec2(-1.0, -1.0),
  vec2( 3.0, -1.0),
  vec2(-1.0,  3.0)
);

out vec2 vUv;

void main() {
  vec2 p = POS[gl_VertexID];
  vUv = p * 0.5 + 0.5;
  gl_Position = vec4(p, 0.0, 1.0);
}
`;

export const DISSOLVE_MASK_FRAG = `#version 300 es
precision highp float;

in vec2 vUv;
out vec4 fragColor;

float hash21(vec2 p) {
  p = fract(p * vec2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

float vnoise2(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  float a = hash21(i);
  float b = hash21(i + vec2(1.0, 0.0));
  float c = hash21(i + vec2(0.0, 1.0));
  float d = hash21(i + vec2(1.0, 1.0));
  return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

void main() {
  // Smooth two-octave noise map drawn once at half resolution
  vec2 q = vUv * 4.5 + vec2(3.7, 1.9);
  float n = (vnoise2(q) * 0.6667 + vnoise2(q * 2.0 + vec2(11.3, 7.1)) * 0.3333);
  fragColor = vec4(n, n, n, 1.0);
}
`;

export const PASS1_DOTS_FRAG = `#version 300 es
precision highp float;

uniform int uPattern;
uniform vec2 uBoxSize;
uniform vec2 uGridOrigin;
uniform vec2 uGridDims;
uniform float uPitch;
uniform float uNoiseScale;
uniform float uClock;

out vec4 fragColor;

const float PI = 3.14159265359;

float sq(float x) {
  return x * x;
}

float hash21(vec2 p) {
  p = fract(p * vec2(127.1, 311.7));
  p += dot(p, p + 34.53);
  return fract(p.x * p.y);
}

float hash31(vec3 p) {
  p = fract(p * vec3(127.1, 311.7, 74.7));
  p += dot(p, p.yzx + 33.33);
  return fract((p.x + p.y) * p.z);
}

float vnoise2(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  float a = hash21(i);
  float b = hash21(i + vec2(1.0, 0.0));
  float c = hash21(i + vec2(0.0, 1.0));
  float d = hash21(i + vec2(1.0, 1.0));
  return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

float vnoise3(vec3 p) {
  vec3 i = floor(p);
  vec3 f = fract(p);
  vec3 u = f * f * (3.0 - 2.0 * f);
  float n000 = hash31(i + vec3(0.0, 0.0, 0.0));
  float n100 = hash31(i + vec3(1.0, 0.0, 0.0));
  float n010 = hash31(i + vec3(0.0, 1.0, 0.0));
  float n110 = hash31(i + vec3(1.0, 1.0, 0.0));
  float n001 = hash31(i + vec3(0.0, 0.0, 1.0));
  float n101 = hash31(i + vec3(1.0, 0.0, 1.0));
  float n011 = hash31(i + vec3(0.0, 1.0, 1.0));
  float n111 = hash31(i + vec3(1.0, 1.0, 1.0));
  float nx00 = mix(n000, n100, u.x);
  float nx10 = mix(n010, n110, u.x);
  float nx01 = mix(n001, n101, u.x);
  float nx11 = mix(n011, n111, u.x);
  return mix(mix(nx00, nx10, u.y), mix(nx01, nx11, u.y), u.z);
}

// 4-octave fBm, 2x frequency & 0.5x amplitude each octave
float fbm2(vec2 p) {
  float sum = 0.0;
  float amp = 0.5;
  for (int i = 0; i < 4; i++) {
    sum += amp * vnoise2(p);
    p *= 2.0;
    amp *= 0.5;
  }
  return sum / 0.9375;
}

float fbm3(vec3 p) {
  float sum = 0.0;
  float amp = 0.5;
  for (int i = 0; i < 4; i++) {
    sum += amp * vnoise3(p);
    p *= 2.0;
    amp *= 0.5;
  }
  return sum / 0.9375;
}

float segDist(vec2 p, vec2 a, vec2 b) {
  vec2 pa = p - a;
  vec2 ba = b - a;
  float h = clamp(dot(pa, ba) / max(dot(ba, ba), 1e-5), 0.0, 1.0);
  return length(pa - ba * h);
}

float evalSnowflakes(vec2 p, float t, float n) {
  float z = max(0.27 * uBoxSize.y, 11.0 * uPitch);
  float flakeVal = 0.0;

  // Six-armed flakes falling at 18px/s with slight horizontal sway
  vec2 fallP = vec2(p.x, p.y - 18.0 * t);
  vec2 baseCell = floor(fallP / z);

  for (int oy = -1; oy <= 1; oy++) {
    for (int ox = -1; ox <= 1; ox++) {
      vec2 cid = baseCell + vec2(float(ox), float(oy));
      float r1 = hash21(cid + vec2(1.7, 3.1));
      float r2 = hash21(cid + vec2(9.2, 5.4));
      float r3 = hash21(cid + vec2(4.8, 8.6));
      float r4 = hash21(cid + vec2(13.1, 2.3));

      float sway = sin(t * 0.85 + r4 * 6.28318) * z * 0.07;
      vec2 fc = (cid + vec2(0.22 + 0.56 * r1, 0.22 + 0.56 * r2)) * z + vec2(sway, 0.0);
      float arm = max(mix(0.37, 0.50, r3) * z, 4.0 * uPitch);
      float rot = r4 * 6.28318 + (r1 - 0.5) * 0.55 * t;

      vec2 d = fallP - fc;
      float dist = length(d);
      if (dist < arm + uPitch * 1.4) {
        // Fold angle into 1/12th of a turn so one arm + branch renders all 6
        float ang = atan(d.y, d.x) - rot;
        float sector = PI / 3.0;
        ang = mod(ang + sector * 0.5, sector) - sector * 0.5;
        ang = abs(ang);
        vec2 fp = vec2(cos(ang), sin(ang)) * dist;

        float dArm = segDist(fp, vec2(0.0), vec2(arm, 0.0));
        vec2 bStart = vec2(arm * 0.5, 0.0);
        vec2 bEnd = bStart + vec2(cos(PI / 3.0), sin(PI / 3.0)) * (arm * 0.36);
        float dBranch = segDist(fp, bStart, bEnd);
        float dFlake = min(dArm, dBranch);

        float strokeW = max(uPitch * 0.58, 5.5);
        float lit = exp(-0.5 * (dFlake / strokeW) * (dFlake / strokeW));
        flakeVal = max(flakeVal, lit);
      }
    }
  }

  // Background single-dot snow: one grain per 0.4z square, falling twice as fast at 35-60%
  float bgZ = max(0.4 * z, uPitch * 2.5);
  vec2 bgP = vec2(p.x, p.y - 36.0 * t);
  vec2 bgCell = floor(bgP / bgZ);
  float br1 = hash21(bgCell + vec2(21.4, 7.9));
  float br2 = hash21(bgCell + vec2(3.3, 17.2));
  float br3 = hash21(bgCell + vec2(11.8, 29.1));
  vec2 bgCenter = (bgCell + vec2(0.15 + 0.7 * br1, 0.15 + 0.7 * br2)) * bgZ;
  float bgDist = length(bgP - bgCenter);
  float bgLit = (1.0 - smoothstep(uPitch * 0.25, uPitch * 0.78, bgDist)) * mix(0.35, 0.60, br3);

  return clamp(max(max(flakeVal, bgLit), 0.12 * n), 0.0, 1.0);
}

void main() {
  vec2 cell = floor(gl_FragCoord.xy);
  if (cell.x >= uGridDims.x || cell.y >= uGridDims.y) {
    fragColor = vec4(0.0);
    return;
  }

  // Exact logical px center of this dot (top-down coordinates)
  vec2 p = uGridOrigin + (cell + 0.5) * uPitch;
  vec2 center = uBoxSize * 0.5;
  float scale = max(uNoiseScale, 20.0);
  float t = uClock;

  // Base 4-octave fBm value noise at p / scale and t * 0.32
  float n = fbm3(vec3(p / scale, t * 0.32));
  float v = n;

  if (uPattern == 0) {
    // Ripple
    float r = length(p - center) / scale;
    float w = 0.5 + 0.5 * sin(r * 5.2 * (PI * 0.5) - 2.6 * t);
    v = smoothstep(0.15, 0.85, mix(n, w, 0.6) * (0.55 + 0.6 * n));
  } else if (uPattern == 1) {
    // fBm breath
    v = smoothstep(0.28, 0.74, n);
  } else if (uPattern == 2) {
    // Double helix in units of boxHeight / 306
    float unit = max(uBoxSize.y, 48.0) / 306.0;
    float x = (p.x - center.x) / unit;
    float y = (p.y - center.y) / unit;
    float phase = x / 44.0 - 1.8 * t;
    float s = sin(phase);
    float c = cos(phase);
    float y1 = 72.0 * s;
    float y2 = -72.0 * s;

    float g1 = exp(-0.5 * sq((y - y1) / 10.0)) * (0.62 + 0.38 * c);
    float g2 = exp(-0.5 * sq((y - y2) / 10.0)) * (0.62 - 0.38 * c);

    float rung = 0.0;
    if (mod(cell.x, 2.0) < 0.5) {
      float lo = min(y1, y2);
      float hi = max(y1, y2);
      if (y > lo + 4.0 && y < hi - 4.0) {
        float span = max(hi - lo, 1.0);
        float edgeSoft = smoothstep(lo, lo + min(14.0, span * 0.35), y)
                       * (1.0 - smoothstep(hi - min(14.0, span * 0.35), hi, y));
        rung = 0.52 * edgeSoft * smoothstep(0.12, 0.55, abs(s));
      }
    }
    v = clamp(max(max(g1, g2), rung) + 0.08 * n, 0.0, 1.0);
  } else if (uPattern == 3) {
    // Static grain: each dot eases between random levels at ~2.5Hz on its own offset, mixed 42% over n
    float off = hash21(cell + vec2(5.1, 11.7));
    float gt = t * 2.5 + off * 17.0;
    float k = floor(gt);
    float frac = smoothstep(0.0, 1.0, fract(gt));
    float g0 = hash31(vec3(cell, k));
    float g1 = hash31(vec3(cell, k + 1.0));
    float grain = mix(g0, g1, frac);
    v = smoothstep(0.10, 0.90, mix(n, grain, 0.42));
  } else if (uPattern == 4) {
    // Spiral arms (3 arms)
    vec2 dp = p - center;
    float r = length(dp) / scale;
    float theta = atan(dp.y, dp.x);
    float s = 0.5 + 0.5 * sin(3.0 * theta + 5.5 * r - 2.2 * t);
    v = smoothstep(0.12, 0.85, mix(n, s, 0.55) * (0.6 + 0.5 * n));
  } else if (uPattern == 5) {
    // Domain warp: noise read at point pushed far by more noise, drifting like smoke
    vec2 q = p / scale;
    vec2 drift = vec2(0.22 * t, -0.10 * t);
    vec2 w = vec2(
      fbm2(q * 1.1 + drift),
      fbm2(q * 1.1 + drift + vec2(5.2, 5.2))
    );
    v = smoothstep(0.34, 0.70, fbm2(q + 4.0 * (w - 0.5) + 1.6 * drift));
  } else if (uPattern == 6) {
    // Sine plasma: 4 sines summed in noise cells
    vec2 q = (p - center) / scale;
    float r = length(q);
    float sSum = sin(2.6 * q.x + 0.9 * t)
               + sin(3.1 * q.y - 1.1 * t)
               + sin(2.2 * (q.x + q.y) + 0.7 * t)
               + sin(4.4 * r - 1.5 * t);
    v = smoothstep(0.20, 0.80, mix(0.5 + 0.125 * sSum, n, 0.35));
  } else if (uPattern == 7) {
    // Light band: Gaussian band 0.15 wide sweeping diagonally every 3.3s over half-strength noise
    float diag = (p.x / max(uBoxSize.x, 1.0) + p.y / max(uBoxSize.y, 1.0)) * 0.5;
    float sweep = fract(t / 3.3) * 1.6 - 0.3;
    float band = exp(-0.5 * sq((diag - sweep) / 0.15));
    v = clamp(0.5 * n + band * (1.0 - 0.3 * n), 0.0, 1.0);
  } else if (uPattern == 8) {
    // Snowflakes
    v = evalSnowflakes(p, t, n);
  }

  fragColor = vec4(v, 0.0, 0.0, 1.0);
}
`;

export const PASS2_COMPOSITE_FRAG = `#version 300 es
precision highp float;

uniform sampler2D uDotField;
uniform sampler2D uImage;
uniform sampler2D uDissolveTex;

uniform vec2 uViewportOrigin; // bottom-left of viewport in framebuffer px
uniform vec2 uBoxSizePx;      // box size in framebuffer px
uniform vec2 uBoxSize;        // box size in logical CSS px
uniform float uDpr;
uniform float uCornerRadius;

uniform vec2 uGridOrigin;
uniform vec2 uGridDims;
uniform vec2 uDotTexSize;
uniform float uPitch;
uniform float uDotSizePct;
uniform float uNoiseScale;

uniform int uReveal;
uniform float uHasImage;      // 0.0 = loading/error only, 1.0 = image ready
uniform float uRevealR;       // 0..1 eased reveal progress
uniform float uDotsFade;      // 0..1 fade in of dots over 500ms
uniform float uSimpleFade;    // 0..1 when reduced-motion or fast-skip fade is active
uniform float uUseSimpleFade; // 1.0 = simple opacity fade (skip zoom/bend/dispersion/shockwave)

uniform float uClock;         // scaled breathing clock (s)
uniform float uTime;          // real elapsed clock (s) for edge glow & 24Hz grain
uniform float uTheme;         // 0.0 = light, 1.0 = dark (crossfades over 480ms)

uniform float uZoomInPct;
uniform float uBlurInPct;
uniform float uDispersionPct;
uniform float uEdgeBendPx;
uniform float uEdgeGlowPct;
uniform float uDotGlowPct;

uniform vec2 uImgSize;        // natural image width/height
uniform int uFitMode;         // 0 = cover, 1 = contain

uniform vec3 uBoxBgLight;
uniform vec3 uBoxBgDark;
uniform vec3 uDotColLight;
uniform vec3 uDotColDark;

out vec4 fragColor;

const float PI = 3.14159265359;

float sq(float x) {
  return x * x;
}

float hash21(vec2 p) {
  p = fract(p * vec2(127.1, 311.7));
  p += dot(p, p + 34.53);
  return fract(p.x * p.y);
}

float hash31(vec3 p) {
  p = fract(p * vec3(127.1, 311.7, 74.7));
  p += dot(p, p.yzx + 33.33);
  return fract((p.x + p.y) * p.z);
}

vec2 hash22(vec2 p) {
  float x = hash21(p);
  float y = hash21(p + vec2(37.2, 17.9));
  return vec2(x, y);
}

float vnoise2(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(hash21(i), hash21(i + vec2(1.0, 0.0)), u.x),
    mix(hash21(i + vec2(0.0, 1.0)), hash21(i + vec2(1.0, 1.0)), u.x),
    u.y
  );
}

// Signed distance to rounded box (negative inside, positive outside)
float sdRoundedBox(vec2 p, vec2 halfSize, float r) {
  vec2 q = abs(p) - halfSize + vec2(r);
  return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r;
}

vec2 sdRoundedBoxNormal(vec2 p, vec2 halfSize, float r) {
  float eps = 0.75;
  float dx = sdRoundedBox(p + vec2(eps, 0.0), halfSize, r) - sdRoundedBox(p - vec2(eps, 0.0), halfSize, r);
  float dy = sdRoundedBox(p + vec2(0.0, eps), halfSize, r) - sdRoundedBox(p - vec2(0.0, eps), halfSize, r);
  vec2 g = vec2(dx, dy);
  float len = length(g);
  return len > 1e-5 ? g / len : vec2(0.0, 0.0);
}

float readDotV(vec2 cell) {
  if (cell.x < 0.0 || cell.y < 0.0 || cell.x >= uGridDims.x || cell.y >= uGridDims.y) {
    return 0.0;
  }
  return texture(uDotField, (cell + 0.5) / uDotTexSize).r;
}

// Map logical box pixel coordinate to image UV (with cover/contain fit)
vec2 pxToImgUv(vec2 px, out vec2 fitSize) {
  float boxAspect = uBoxSize.x / max(uBoxSize.y, 1.0);
  float imgAspect = uImgSize.x / max(uImgSize.y, 1.0);
  if (uFitMode == 1) {
    // contain
    if (boxAspect > imgAspect) {
      fitSize = vec2(uBoxSize.y * imgAspect, uBoxSize.y);
    } else {
      fitSize = vec2(uBoxSize.x, uBoxSize.x / max(imgAspect, 1e-4));
    }
  } else {
    // cover
    if (boxAspect > imgAspect) {
      fitSize = vec2(uBoxSize.x, uBoxSize.x / max(imgAspect, 1e-4));
    } else {
      fitSize = vec2(uBoxSize.y * imgAspect, uBoxSize.y);
    }
  }
  vec2 offset = (uBoxSize - fitSize) * 0.5;
  return (px - offset) / max(fitSize, vec2(1.0));
}

vec4 samplePictureAt(vec2 px, float mipBias, vec3 boxBg) {
  vec2 fitSize;
  vec2 uv = pxToImgUv(px, fitSize);
  if (uFitMode == 1) {
    // Smooth 1px edge against letterbox matte so contained product images never smear
    vec2 edgeDist = min(uv, 1.0 - uv) * fitSize;
    float inside = smoothstep(-0.5, 0.5, min(edgeDist.x, edgeDist.y));
    if (inside <= 0.001) {
      return vec4(boxBg, 1.0);
    }
    vec4 tex = textureLod(uImage, clamp(uv, 0.001, 0.999), max(mipBias, 0.0));
    vec3 rgb = mix(boxBg, tex.rgb, tex.a * inside);
    return vec4(rgb, 1.0);
  }
  vec4 tex = textureLod(uImage, clamp(uv, 0.001, 0.999), max(mipBias, 0.0));
  return vec4(mix(boxBg, tex.rgb, tex.a), 1.0);
}

vec3 sampleWithDispersion(vec2 basePx, vec2 dispPx, float mipBias, float dispAmt, vec3 boxBg) {
  float moveLen = length(dispPx);
  if (moveLen < 0.02 || dispAmt <= 0.001) {
    return samplePictureAt(basePx + dispPx, mipBias, boxBg).rgb;
  }
  float r = samplePictureAt(basePx + dispPx * (1.0 + dispAmt), mipBias, boxBg).r;
  float g = samplePictureAt(basePx + dispPx, mipBias, boxBg).g;
  float b = samplePictureAt(basePx + dispPx * (1.0 - dispAmt), mipBias, boxBg).b;
  return vec3(r, g, b);
}

vec3 shockwaveGradient(float u) {
  vec3 c0 = vec3(0.3137, 0.5490, 1.0000); // #508CFF
  vec3 c1 = vec3(0.6275, 0.4314, 1.0000); // #A06EFF
  vec3 c2 = vec3(1.0000, 0.4314, 0.7059); // #FF6EB4
  vec3 c3 = vec3(1.0000, 0.7451, 0.5490); // #FFBE8C
  if (u < 0.3333) return mix(c0, c1, u * 3.0);
  if (u < 0.6667) return mix(c1, c2, (u - 0.3333) * 3.0);
  return mix(c2, c3, (u - 0.6667) * 3.0);
}

void main() {
  // Logical top-down coordinates inside this box (0..uBoxSize)
  vec2 localFb = gl_FragCoord.xy - uViewportOrigin;
  vec2 fragPx = vec2(localFb.x, uBoxSizePx.y - localFb.y) / max(uDpr, 1.0);

  vec2 center = uBoxSize * 0.5;
  float rCorner = min(uCornerRadius, min(center.x, center.y));
  float sd = sdRoundedBox(fragPx - center, center, rCorner);
  float boxMask = 1.0 - smoothstep(-0.6, 0.6, sd);
  if (boxMask <= 0.001) {
    discard;
  }
  // Positive inward distance from box edge in logical px
  float dEdge = max(-sd, 0.0);

  vec3 boxBg = mix(uBoxBgLight, uBoxBgDark, uTheme);
  vec3 dotCol = mix(uDotColLight, uDotColDark, uTheme);

  // ----- 1. Procedural dot grid + vignette + dark-only halos & edge glow -----
  vec2 cell = floor((fragPx - uGridOrigin) / uPitch);
  vec2 dotCenter = uGridOrigin + (cell + 0.5) * uPitch;
  float v = readDotV(cell);
  float vig = 0.45 + 0.55 * smoothstep(8.0, 72.0, dEdge);

  float maxRad = uPitch * (uDotSizePct * 0.01);
  float dotRad = mix(0.55, maxRad, v);
  float distToDot = length(fragPx - dotCenter);
  float dotShape = 1.0 - smoothstep(dotRad - 0.55, dotRad + 0.55, distToDot);
  float dotOpacity = mix(0.10, 1.0, v) * vig * uDotsFade;

  vec3 dotsLayer = mix(boxBg, dotCol, dotShape * dotOpacity);

  // Dark-only dot halo summed over own cell + 8 neighbors (sigma = 0.3 * pitch, strength v^2)
  if (uTheme > 0.005 && uDotGlowPct > 0.01 && uDotsFade > 0.005) {
    float sigma = 0.3 * uPitch;
    float invTwoSig2 = 1.0 / max(2.0 * sigma * sigma, 1e-4);
    float haloSum = 0.0;
    for (int oy = -1; oy <= 1; oy++) {
      for (int ox = -1; ox <= 1; ox++) {
        vec2 nc = cell + vec2(float(ox), float(oy));
        float nv = readDotV(nc);
        if (nv > 0.01) {
          vec2 nPos = uGridOrigin + (nc + 0.5) * uPitch;
          vec2 delta = fragPx - nPos;
          haloSum += exp(-dot(delta, delta) * invTwoSig2) * (nv * nv);
        }
      }
    }
    float haloStrength = haloSum * vig * uTheme * (uDotGlowPct * 0.01) * 0.65 * uDotsFade;
    dotsLayer = clamp(dotsLayer + dotCol * haloStrength, 0.0, 1.0);
  }

  // Dark-only dual-layer counter-phase edge glow while loading (fades out over first third of reveal)
  float edgeGlowFade = 1.0 - smoothstep(0.0, 0.3333, uRevealR);
  if (uTheme > 0.005 && uEdgeGlowPct > 0.01 && edgeGlowFade > 0.005 && uDotsFade > 0.005) {
    float ang = atan(fragPx.y - center.y, fragPx.x - center.x);

    // Outer layer: wide haze exp(-d/12) + thin rim exp(-d/2.5), breathing 2.4s, 2 arcs CW in 8s
    float outerShape = 0.52 * exp(-dEdge / 12.0) + 0.72 * exp(-dEdge / 2.5);
    float outerBreath = 0.65 + 0.35 * sin(2.0 * PI * uTime / 2.4);
    float outerArcs = 0.52 + 0.48 * sq(0.5 + 0.5 * cos(2.0 * (ang - 2.0 * PI * uTime / 8.0)));
    float outer = outerShape * outerBreath * outerArcs;

    // Inner layer: soft band ~6px in (Gaussian 3.5px) + faint wide haze, breathing 3.7s opposite phase, 3 arcs CCW in 11s
    float innerShape = 0.58 * exp(-0.5 * sq((dEdge - 6.0) / 3.5)) + 0.22 * exp(-dEdge / 22.0);
    float innerBreath = 0.65 + 0.35 * sin(2.0 * PI * uTime / 3.7 + PI);
    float innerArcs = 0.50 + 0.50 * sq(0.5 + 0.5 * cos(3.0 * (ang + 2.0 * PI * uTime / 11.0)));
    float inner = innerShape * innerBreath * innerArcs;

    float g = (outer + inner) * (uEdgeGlowPct * 0.01) * uTheme * edgeGlowFade * uDotsFade * 0.55;
    vec3 glowTint = mix(vec3(0.76, 0.86, 1.0), vec3(1.0, 0.96, 0.94), 0.35);
    dotsLayer = 1.0 - (1.0 - dotsLayer) * (1.0 - clamp(glowTint * g, 0.0, 0.85));
  }

  // If no image yet (loading / error frozen), output dots only
  if (uHasImage < 0.5) {
    fragColor = vec4(dotsLayer * boxMask, boxMask);
    return;
  }

  // Simple fade path (reduced motion or fast image < 300ms)
  if (uUseSimpleFade > 0.5) {
    vec3 imgPlain = samplePictureAt(fragPx, 0.0, boxBg).rgb;
    vec3 col = mix(dotsLayer, imgPlain, clamp(uSimpleFade, 0.0, 1.0));
    fragColor = vec4(col * boxMask, boxMask);
    return;
  }

  // ----- 2. Full Reveal Pipeline (Section 4 & 5) -----
  float R = clamp(uRevealR, 0.0, 1.0);
  if (R >= 0.9999) {
    vec3 settled = samplePictureAt(fragPx, 0.0, boxBg).rgb;
    fragColor = vec4(settled * boxMask, boxMask);
    return;
  }

  float oneMinusR = max(1.0 - R, 1e-6);
  float halfDiag = max(length(center), 1.0);
  float dispAmt = uDispersionPct * 0.01;
  float bend = uEdgeBendPx;

  // Shared zoom in: read picture enlarged at 1 + zoom * (1 - R), box never scales
  float zoomScale = 1.0 + (uZoomInPct * 0.01) * (1.0 - R);
  vec2 zoomedPx = center + (fragPx - center) / zoomScale;

  // Shared blur in: 3 * (1-R)^0.8 at default 50% (skipped by Halftone and Denoise)
  float maxBlurMip = (uBlurInPct * 0.01) * 6.0;
  float mipBias = (uReveal == 1 || uReveal == 3) ? 0.0 : maxBlurMip * pow(oneMinusR, 0.8);

  // Shared glass bevel edge bend in last 26px from box rim (reads from further inside)
  vec2 totalDisp = vec2(0.0);
  if (dEdge < 26.0 && bend > 0.001) {
    vec2 outNorm = sdRoundedBoxNormal(fragPx - center, center, rCorner);
    float f = 1.0 - dEdge / 26.0;
    float edgeMag = bend * 2.0 * pow(oneMinusR, 0.7) * (f * f);
    // Push sample point inward (-outNorm) so picture stretches toward the edge
    totalDisp += (-outNorm) * edgeMag;
  }

  // Reveal mask a, front distance (px inside open area), front inward normal, and front line intensity
  float a = 0.0;
  float frontDistPx = 999.0;
  vec2 frontInwardDir = vec2(0.0);
  float frontLineMask = 0.0;
  bool useShockwave = (uReveal == 1 || uReveal == 3 || uReveal == 8);
  bool isHalftone = (uReveal == 1);
  bool isDenoise = (uReveal == 3);
  bool isStream = (uReveal == 7);

  if (uReveal == 0) {
    // Fisheye lens
    float front = (1.3 * R - 0.1) * halfDiag;
    vec2 dp = fragPx - center;
    float dist = length(dp);
    vec2 radDir = dist > 1e-4 ? dp / dist : vec2(0.0);
    a = front > 0.0 ? (1.0 - smoothstep(front - 1.5, front + 1.5, dist)) : 0.0;
    frontDistPx = max(front - dist, 0.0);
    frontInwardDir = -radDir;
    frontLineMask = front > 1.0 ? exp(-sq((dist - front) / 1.4)) : 0.0;

    if (front > 1.0 && dist < front + 4.0) {
      float u = clamp(dist / front, 0.0, 1.0);
      float k = 0.5 * (1.0 - R);
      // Bulge: read at center + d * (1 - k + k * u^2)
      vec2 bulgeDisp = dp * (-k * (1.0 - u * u));
      // Damped wave behind the edge
      float behind = max(front - dist, 0.0);
      float dampWave = sin(behind * 0.24) * exp(-behind / 26.0) * bend * 1.35 * (1.0 - R);
      totalDisp += bulgeDisp + radDir * dampWave;
    }
  } else if (uReveal == 1) {
    // Halftone grow (handled below after shockwave, sets mask a cleanly)
    a = smoothstep(0.0, 0.05, R);
  } else if (uReveal == 2) {
    // Unzip: |y| < R * (halfH + 30) + 22 * sin(x/44 - 1.8t) * (1 - R) in helix units
    float unit = max(uBoxSize.y, 48.0) / 306.0;
    float wave = 22.0 * sin((fragPx.x - center.x) / (44.0 * unit) - 1.8 * uClock) * (1.0 - R) * unit;
    float lim = R * (center.y + 30.0) + wave;
    float ay = abs(fragPx.y - center.y);
    a = 1.0 - smoothstep(lim - 1.4, lim + 1.4, ay);
    frontDistPx = max(lim - ay, 0.0);
    frontInwardDir = vec2(0.0, fragPx.y >= center.y ? -1.0 : 1.0);
    frontLineMask = lim > 0.5 ? exp(-sq((ay - lim) / 1.4)) : 0.0;
  } else if (uReveal == 3) {
    // Denoise: diffusion run backwards
    a = smoothstep(0.0, 0.18, R);
    mipBias = 6.0 * pow(oneMinusR, 1.4);
  } else if (uReveal == 4) {
    // Spiral wipe
    vec2 dp = fragPx - center;
    float dist = length(dp);
    float ang = mod(atan(dp.y, dp.x) + 2.0 * PI, 2.0 * PI);
    float key = 0.72 * (dist / halfDiag) + 0.28 * (ang / (2.0 * PI));
    float thr = 1.12 * R - 0.06;
    float aa = max(1.8 / halfDiag, 0.008);
    a = 1.0 - smoothstep(thr - aa, thr + aa, key);
    frontDistPx = max((thr - key) * halfDiag, 0.0);
    vec2 radDir = dist > 1e-4 ? dp / dist : vec2(1.0, 0.0);
    vec2 tanDir = vec2(-radDir.y, radDir.x);
    frontInwardDir = -normalize(radDir * 0.72 + tanDir * 0.28);
    frontLineMask = thr > 0.0 ? exp(-sq((key - thr) / (aa * 1.1))) : 0.0;
  } else if (uReveal == 5) {
    // Noise dissolve (static half-res 2-octave texture)
    vec2 mUv = fragPx / max(uBoxSize, vec2(1.0));
    float m = texture(uDissolveTex, mUv).r;
    float thr = 1.3 * R - 0.15;
    a = 1.0 - smoothstep(thr - 0.03, thr + 0.03, m);
    float e = 2.0 / max(min(uBoxSize.x, uBoxSize.y), 16.0);
    float gx = texture(uDissolveTex, mUv + vec2(e, 0.0)).r - texture(uDissolveTex, mUv - vec2(e, 0.0)).r;
    float gy = texture(uDissolveTex, mUv + vec2(0.0, e)).r - texture(uDissolveTex, mUv - vec2(0.0, e)).r;
    vec2 grad = vec2(gx, gy);
    float gLen = max(length(grad), 0.002);
    frontDistPx = max((thr - m) * 260.0, 0.0);
    frontInwardDir = -grad / gLen;
    frontLineMask = exp(-sq((m - thr) / 0.022)) * smoothstep(0.01, 0.08, R);
  } else if (uReveal == 6) {
    // Interference wipe (4-sine plasma sum still moving)
    float scale = max(uNoiseScale, 20.0);
    vec2 q = (fragPx - center) / scale;
    float r = length(q);
    float t = uClock;
    float sSum = sin(2.6 * q.x + 0.9 * t)
               + sin(3.1 * q.y - 1.1 * t)
               + sin(2.2 * (q.x + q.y) + 0.7 * t)
               + sin(4.4 * r - 1.5 * t);
    float m = 0.5 + 0.125 * sSum;
    float thr = 1.25 * R - 0.10;
    a = 1.0 - smoothstep(thr - 0.028, thr + 0.028, m);
    // Analytical gradient of sSum wrt q
    vec2 radQ = r > 1e-4 ? q / r : vec2(0.0);
    vec2 g = vec2(
      2.6 * cos(2.6 * q.x + 0.9 * t) + 2.2 * cos(2.2 * (q.x + q.y) + 0.7 * t),
      3.1 * cos(3.1 * q.y - 1.1 * t) + 2.2 * cos(2.2 * (q.x + q.y) + 0.7 * t)
    ) + 4.4 * cos(4.4 * r - 1.5 * t) * radQ;
    float gLen = max(length(g), 0.01);
    frontDistPx = max((thr - m) * scale * 2.4, 0.0);
    frontInwardDir = -g / gLen;
    frontLineMask = exp(-sq((m - thr) / 0.02)) * smoothstep(0.01, 0.08, R);
  } else if (uReveal == 7) {
    // Top-down stream
    float frontY = (1.22 * R - 0.06) * uBoxSize.y;
    a = 1.0 - smoothstep(frontY - 1.5, frontY + 1.5, fragPx.y);
    frontDistPx = max(frontY - fragPx.y, 0.0);
    frontInwardDir = vec2(0.0, -1.0);
    frontLineMask = frontY > 0.5 ? exp(-sq((fragPx.y - frontY) / 1.4)) : 0.0;
  } else if (uReveal == 8) {
    // Cell bloom: Voronoi cells ~1/5 of box height
    float cellSz = max(uBoxSize.y * 0.20, 22.0);
    vec2 gp = fragPx / cellSz;
    vec2 baseC = floor(gp);
    float bestDist = 1e9;
    vec2 bestSeed = vec2(0.0);
    vec2 bestId = vec2(0.0);
    for (int oy = -1; oy <= 1; oy++) {
      for (int ox = -1; ox <= 1; ox++) {
        vec2 cid = baseC + vec2(float(ox), float(oy));
        vec2 seed = (cid + 0.15 + 0.70 * hash22(cid + vec2(4.2, 9.1))) * cellSz;
        float d = length(fragPx - seed);
        if (d < bestDist) {
          bestDist = d;
          bestSeed = seed;
          bestId = cid;
        }
      }
    }
    float t0 = hash21(bestId + vec2(17.3, 2.9)) * 0.55;
    float localR = clamp((R - t0) / max(1.0 - t0, 0.22), 0.0, 1.0);
    float openRad = localR * cellSz * 1.18;
    a = localR > 0.001 ? (1.0 - smoothstep(openRad - 1.5, openRad + 1.5, bestDist)) : 0.0;
    frontDistPx = max(openRad - bestDist, 0.0);
    frontInwardDir = bestDist > 1e-4 ? -(fragPx - bestSeed) / bestDist : vec2(0.0);
    frontLineMask = (localR > 0.01 && localR < 0.99) ? exp(-sq((bestDist - openRad) / 1.4)) : 0.0;
  }

  // Front glass bend (20px inside the reveal front with +-67% slow noise wobble, fades past R=0.85)
  float frontFade = 1.0 - smoothstep(0.85, 1.0, R);
  if (frontDistPx < 20.0 && bend > 0.001 && frontFade > 0.001 && length(frontInwardDir) > 0.01) {
    float wob = 1.0 + 0.67 * (2.0 * vnoise2(fragPx / 85.0 + vec2(uTime * 0.18, -uTime * 0.12)) - 1.0);
    float fMag = bend * exp(-sq(frontDistPx / 14.0)) * wob * frontFade;
    totalDisp += frontInwardDir * fMag;
  }

  // Shockwave on Halftone grow, Denoise, Cell bloom
  float shockGlowStrength = 0.0;
  vec3 shockGlowColor = vec3(0.0);
  if (useShockwave) {
    float ringR = R * 1.25 * halfDiag;
    float ringW = max(0.12 * halfDiag, 10.0);
    vec2 dp = fragPx - center;
    float dist = length(dp);
    vec2 radDir = dist > 1e-4 ? dp / dist : vec2(0.0);
    float x = (dist - ringR) / ringW;
    float z = 0.75 * x;
    float envA = smoothstep(0.0, 0.08, R) * (1.0 - smoothstep(0.55, 1.0, R));
    float push = bend * 1.6 * 2.33 * z * exp(-z * z) * envA;
    totalDisp += radDir * push;

    float ringGauss = envA * exp(-0.5 * x * x);
    shockGlowStrength = ringGauss * mix(0.12, 0.16, uTheme);
    shockGlowColor = shockwaveGradient(clamp((x + 1.5) / 3.0, 0.0, 1.0));
  }

  // Top-down stream coarse band (16% just above front: 2px blocks growing to 16px at front)
  vec2 effectiveBasePx = zoomedPx;
  if (isStream) {
    float frontY = (1.22 * R - 0.06) * uBoxSize.y;
    float bandH = max(0.16 * uBoxSize.y, 12.0);
    float distAbove = frontY - fragPx.y;
    if (distAbove > 0.0 && distAbove < bandH) {
      float frac = 1.0 - distAbove / bandH; // 0 at top of band, 1 right at front
      float blk = mix(2.0, 16.0, frac);
      effectiveBasePx = (floor(zoomedPx / blk) + 0.5) * blk;
    }
  }

  vec3 finalRgb;

  if (isHalftone) {
    // Halftone grow: each dot takes image color averaged over its cell & swells to half pitch until touching,
    // then full image focuses in from 4.5 mip levels soft over second half.
    float growT = smoothstep(0.0, 0.55, R);
    float focusT = smoothstep(0.45, 1.0, R);

    vec2 cellZoomPx = center + (dotCenter - center) / zoomScale;
    float cellMip = max(log2(max(uPitch * uDpr, 1.0)), 1.0);
    vec3 cellImgCol = sampleWithDispersion(cellZoomPx, totalDisp, cellMip, dispAmt, boxBg);

    float targetRad = uPitch * 0.72; // covers square cell corners cleanly once circles touch
    float hRad = mix(dotRad, targetRad, growT);
    float hCircle = 1.0 - smoothstep(hRad - 0.65, hRad + 0.65, distToDot);

    // Blend dot color from neutral dot into cell's averaged image color as it swells
    vec3 swellDotCol = mix(dotCol, cellImgCol, smoothstep(0.0, 0.35, growT));
    vec3 halftoneStage = mix(dotsLayer, swellDotCol, hCircle * smoothstep(0.0, 0.12, R));

    // Second half: focus picture in from 4.5 mip levels soft
    float htMip = 4.5 * (1.0 - focusT);
    vec3 focusedImg = sampleWithDispersion(effectiveBasePx, totalDisp, htMip, dispAmt, boxBg);

    vec3 imgComposite = mix(halftoneStage, focusedImg, focusT);
    // Apply shockwave glow ONLY on revealed image part (never on unrevealed dots!)
    float revealedPart = max(hCircle * smoothstep(0.0, 0.2, R), focusT);
    if (shockGlowStrength > 0.001 && revealedPart > 0.001) {
      float s = shockGlowStrength * revealedPart;
      vec3 darkScreen = 1.0 - (1.0 - imgComposite) * (1.0 - shockGlowColor * s);
      vec3 lightTint = mix(imgComposite, shockGlowColor, s);
      imgComposite = mix(lightTint, darkScreen, uTheme);
    }
    finalRgb = imgComposite;
  } else {
    vec3 imgCol = sampleWithDispersion(effectiveBasePx, totalDisp, mipBias, dispAmt, boxBg);

    // Denoise: 1.5px grain of +-45% * (1 - R)^2 refreshing 24 times/sec
    if (isDenoise) {
      float tick = floor(uTime * 24.0);
      vec2 gCell = floor(fragPx / 1.5);
      vec3 gNoise = vec3(
        hash31(vec3(gCell, tick)),
        hash31(vec3(gCell + vec2(17.1, 3.7), tick)),
        hash31(vec3(gCell + vec2(5.9, 29.3), tick))
      ) * 2.0 - 1.0;
      float amp = 0.45 * sq(1.0 - R);
      imgCol = clamp(imgCol + gNoise * amp, 0.0, 1.0);
    }

    // Shockwave glow ONLY on revealed part of image, NEVER on the dots
    if (useShockwave && shockGlowStrength > 0.001 && a > 0.001) {
      float s = shockGlowStrength * a;
      vec3 darkScreen = 1.0 - (1.0 - imgCol) * (1.0 - shockGlowColor * s);
      vec3 lightTint = mix(imgCol, shockGlowColor, s);
      imgCol = mix(lightTint, darkScreen, uTheme);
    }

    finalRgb = mix(dotsLayer, imgCol, a);

    // Plain neutral white front line (0.55 dark, 0.22 light), fading past R = 0.85, never colored
    float lineAlpha = frontLineMask * mix(0.22, 0.55, uTheme) * frontFade;
    finalRgb = mix(finalRgb, vec3(1.0), clamp(lineAlpha, 0.0, 1.0));
  }

  fragColor = vec4(finalRgb * boxMask, boxMask);
}
`;
