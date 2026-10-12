import { DEFAULT_ADJUSTMENTS, type EnhancementAdjustments } from "./types";

export type NormalizedShaderUniforms = {
  brightness: number;
  contrast: number;
  gamma: number;
  saturation: number;
  sharpness: number;
  blackLevel: number;
  whiteLevel: number;
  temperature: number;
};

/**
 * Generate CSS filter string as a hardware-friendly fallback when WebGL is unavailable.
 */
export function generateCssFilterString(adj?: EnhancementAdjustments | null): string {
  if (!adj) return "none";
  const parts: string[] = [];

  if (Math.abs(adj.brightness) > 0.5) {
    parts.push(`brightness(${Math.round(100 + adj.brightness)}%)`);
  }
  if (Math.abs(adj.contrast) > 0.5) {
    parts.push(`contrast(${Math.round(100 + adj.contrast)}%)`);
  }
  if (Math.abs(adj.saturation) > 0.5) {
    parts.push(`saturate(${Math.round(100 + adj.saturation)}%)`);
  }

  return parts.length > 0 ? parts.join(" ") : "none";
}

const VERTEX_SHADER_SRC = `
attribute vec2 aPosition;
attribute vec2 aTexCoord;
varying vec2 vTexCoord;

void main() {
  gl_Position = vec4(aPosition, 0.0, 1.0);
  vTexCoord = aTexCoord;
}
`;

const FRAGMENT_SHADER_SRC = `
precision mediump float;
uniform sampler2D uTexture;
uniform vec2 uTexelSize;
uniform float uBrightness;
uniform float uContrast;
uniform float uGamma;
uniform float uSaturation;
uniform float uSharpness;
uniform float uBlackLevel;
uniform float uWhiteLevel;
uniform float uTemperature;
varying vec2 vTexCoord;

void main() {
  vec4 base = texture2D(uTexture, vTexCoord);
  vec3 color = base.rgb;

  // 1. Unsharp mask sharpening (4-tap Laplacian)
  if (uSharpness > 0.01) {
    vec3 n1 = texture2D(uTexture, vTexCoord + vec2(0.0, uTexelSize.y)).rgb;
    vec3 n2 = texture2D(uTexture, vTexCoord - vec2(0.0, uTexelSize.y)).rgb;
    vec3 n3 = texture2D(uTexture, vTexCoord + vec2(uTexelSize.x, 0.0)).rgb;
    vec3 n4 = texture2D(uTexture, vTexCoord - vec2(uTexelSize.x, 0.0)).rgb;
    vec3 neighborAvg = (n1 + n2 + n3 + n4) * 0.25;
    color = clamp(color + (color - neighborAvg) * (uSharpness * 1.5), 0.0, 1.0);
  }

  // 2. White balance / temperature adjustment
  if (abs(uTemperature) > 0.01) {
    color.r = clamp(color.r + uTemperature * 0.15, 0.0, 1.0);
    color.b = clamp(color.b - uTemperature * 0.15, 0.0, 1.0);
  }

  // 3. Black & white levels normalization
  color = clamp((color - uBlackLevel) / max(0.01, uWhiteLevel - uBlackLevel), 0.0, 1.0);

  // 4. Gamma curve
  if (abs(uGamma - 1.0) > 0.01) {
    color = pow(color, vec3(1.0 / max(0.1, uGamma)));
  }

  // 5. Contrast & Brightness
  color = (color - 0.5) * (1.0 + uContrast) + 0.5 + uBrightness;
  color = clamp(color, 0.0, 1.0);

  // 6. Saturation
  float luma = dot(color, vec3(0.2126, 0.7152, 0.0722));
  color = mix(vec3(luma), color, max(0.0, 1.0 + uSaturation));

  gl_FragColor = vec4(clamp(color, 0.0, 1.0), base.a);
}
`;

export class WebGLVisualEnhancer {
  private gl: WebGLRenderingContext | null = null;
  private program: WebGLProgram | null = null;
  private texture: WebGLTexture | null = null;
  private vbo: WebGLBuffer | null = null;
  private isFallback = false;

  private uTextureLoc: WebGLUniformLocation | null = null;
  private uTexelSizeLoc: WebGLUniformLocation | null = null;
  private uBrightnessLoc: WebGLUniformLocation | null = null;
  private uContrastLoc: WebGLUniformLocation | null = null;
  private uGammaLoc: WebGLUniformLocation | null = null;
  private uSaturationLoc: WebGLUniformLocation | null = null;
  private uSharpnessLoc: WebGLUniformLocation | null = null;
  private uBlackLevelLoc: WebGLUniformLocation | null = null;
  private uWhiteLevelLoc: WebGLUniformLocation | null = null;
  private uTemperatureLoc: WebGLUniformLocation | null = null;

  normalizeUniforms(adj: EnhancementAdjustments): NormalizedShaderUniforms {
    return {
      brightness: Math.max(-1.0, Math.min(1.0, adj.brightness / 100)),
      contrast: Math.max(-1.0, Math.min(1.0, adj.contrast / 100)),
      gamma: Math.max(0.2, Math.min(3.0, adj.gamma)),
      saturation: Math.max(-1.0, Math.min(1.0, adj.saturation / 100)),
      sharpness: Math.max(0.0, Math.min(1.0, adj.sharpness / 100)),
      blackLevel: Math.max(0.0, Math.min(0.5, adj.blackLevel)),
      whiteLevel: Math.max(0.5, Math.min(1.0, adj.whiteLevel)),
      temperature: Math.max(-1.0, Math.min(1.0, adj.temperature / 50)),
    };
  }

  init(canvas: HTMLCanvasElement): boolean {
    try {
      this.gl = (canvas.getContext("webgl", {
        preserveDrawingBuffer: false,
        alpha: false,
        powerPreference: "low-power",
      }) ||
        canvas.getContext("experimental-webgl")) as WebGLRenderingContext | null;

      if (!this.gl) {
        this.isFallback = true;
        return false;
      }

      const gl = this.gl;
      const vs = this.compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER_SRC);
      const fs = this.compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER_SRC);

      if (!vs || !fs) {
        this.isFallback = true;
        return false;
      }

      const program = gl.createProgram();
      if (!program) {
        this.isFallback = true;
        return false;
      }

      gl.attachShader(program, vs);
      gl.attachShader(program, fs);
      gl.linkProgram(program);

      if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
        this.isFallback = true;
        return false;
      }

      this.program = program;

      // Uniforms
      this.uTextureLoc = gl.getUniformLocation(program, "uTexture");
      this.uTexelSizeLoc = gl.getUniformLocation(program, "uTexelSize");
      this.uBrightnessLoc = gl.getUniformLocation(program, "uBrightness");
      this.uContrastLoc = gl.getUniformLocation(program, "uContrast");
      this.uGammaLoc = gl.getUniformLocation(program, "uGamma");
      this.uSaturationLoc = gl.getUniformLocation(program, "uSaturation");
      this.uSharpnessLoc = gl.getUniformLocation(program, "uSharpness");
      this.uBlackLevelLoc = gl.getUniformLocation(program, "uBlackLevel");
      this.uWhiteLevelLoc = gl.getUniformLocation(program, "uWhiteLevel");
      this.uTemperatureLoc = gl.getUniformLocation(program, "uTemperature");

      // VBO: Fullscreen quad positions + texcoords
      // [x, y, u, v]
      // Video textures in WebGL: top-left (0, 0)
      const quad = new Float32Array([
        -1.0, -1.0, 0.0, 1.0,
         1.0, -1.0, 1.0, 1.0,
        -1.0,  1.0, 0.0, 0.0,
        -1.0,  1.0, 0.0, 0.0,
         1.0, -1.0, 1.0, 1.0,
         1.0,  1.0, 1.0, 0.0,
      ]);

      this.vbo = gl.createBuffer();
      gl.bindBuffer(gl.ARRAY_BUFFER, this.vbo);
      gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);

      const aPosition = gl.getAttribLocation(program, "aPosition");
      const aTexCoord = gl.getAttribLocation(program, "aTexCoord");

      gl.enableVertexAttribArray(aPosition);
      gl.vertexAttribPointer(aPosition, 2, gl.FLOAT, false, 16, 0);

      gl.enableVertexAttribArray(aTexCoord);
      gl.vertexAttribPointer(aTexCoord, 2, gl.FLOAT, false, 16, 8);

      // Texture
      this.texture = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, this.texture);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);

      return true;
    } catch {
      this.isFallback = true;
      return false;
    }
  }

  private compileShader(gl: WebGLRenderingContext, type: number, src: string): WebGLShader | null {
    const shader = gl.createShader(type);
    if (!shader) return null;
    gl.shaderSource(shader, src);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      gl.deleteShader(shader);
      return null;
    }
    return shader;
  }

  render(source: HTMLVideoElement | HTMLCanvasElement, adj: EnhancementAdjustments = DEFAULT_ADJUSTMENTS): void {
    if (this.isFallback || !this.gl || !this.program || !this.texture) {
      return;
    }

    const gl = this.gl;
    const w = "videoWidth" in source ? (source.videoWidth as number) : (source.width as number);
    const h = "videoHeight" in source ? (source.videoHeight as number) : (source.height as number);
    if (!w || !h) return;

    if (gl.canvas.width !== w || gl.canvas.height !== h) {
      gl.canvas.width = w;
      gl.canvas.height = h;
      gl.viewport(0, 0, w, h);
    }

    gl.useProgram(this.program);

    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.texture);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, source);
    gl.uniform1i(this.uTextureLoc, 0);

    const uniforms = this.normalizeUniforms(adj);
    gl.uniform2f(this.uTexelSizeLoc, 1.0 / w, 1.0 / h);
    gl.uniform1f(this.uBrightnessLoc, uniforms.brightness);
    gl.uniform1f(this.uContrastLoc, uniforms.contrast);
    gl.uniform1f(this.uGammaLoc, uniforms.gamma);
    gl.uniform1f(this.uSaturationLoc, uniforms.saturation);
    gl.uniform1f(this.uSharpnessLoc, uniforms.sharpness);
    gl.uniform1f(this.uBlackLevelLoc, uniforms.blackLevel);
    gl.uniform1f(this.uWhiteLevelLoc, uniforms.whiteLevel);
    gl.uniform1f(this.uTemperatureLoc, uniforms.temperature);

    gl.drawArrays(gl.TRIANGLES, 0, 6);
  }

  destroy(): void {
    if (this.gl && this.program) {
      if (this.texture) this.gl.deleteTexture(this.texture);
      if (this.vbo) this.gl.deleteBuffer(this.vbo);
      this.gl.deleteProgram(this.program);
    }
    this.gl = null;
    this.program = null;
    this.texture = null;
    this.vbo = null;
    this.isFallback = true;
  }
}
