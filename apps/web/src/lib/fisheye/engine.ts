import {
  toRad,
} from "./math";
import type {
  FisheyeCalibration,
  FisheyeMountType,
  FisheyeProjectionModel,
  FisheyeViewMode,
  VirtualFisheyeView,
} from "./types";

const VERTEX_SHADER = `
attribute vec2 aPosition;
varying vec2 vCoord;

void main() {
  gl_Position = vec4(aPosition, 0.0, 1.0);
  vCoord = aPosition;
}
`;

const FRAGMENT_SHADER = `
precision highp float;

varying vec2 vCoord;

uniform sampler2D uTexture;
uniform vec2 uResolution;
uniform vec2 uCenter;
uniform float uRadius;
uniform float uRotation;
uniform float uLensFov;
uniform int uProjectionModel; // 0: equidistant, 1: equisolid, 2: stereographic, 3: orthographic
uniform int uMountType;       // 0: ceiling, 1: wall, 2: floor
uniform int uMode;            // 0: original, 1: rectilinear/ptz, 2: panorama_180, 3: cylindrical_360
uniform vec3 uViewAngles;     // yaw, pitch, roll in radians
uniform float uVirtualFov;    // radians
uniform float uAspectRatio;   // canvas aspect ratio
uniform float uTextureAspect; // source video/image aspect ratio (width / height)
uniform float uWrapScroll;

const float PI = 3.14159265358979323846;

// Rotation helpers
vec3 rotateRoll(vec3 v, float angle) {
  float c = cos(angle);
  float s = sin(angle);
  return vec3(v.x * c - v.y * s, v.x * s + v.y * c, v.z);
}

vec3 rotatePitch(vec3 v, float angle) {
  float c = cos(angle);
  float s = sin(angle);
  return vec3(v.x, v.y * c + v.z * s, -v.y * s + v.z * c);
}

vec3 rotateYaw(vec3 v, float angle) {
  float c = cos(angle);
  float s = sin(angle);
  return vec3(v.x * c + v.z * s, v.y, -v.x * s + v.z * c);
}

// Compute sensor radius r from incidence angle theta
float computeRadius(float theta, float maxTheta, float maxRadius, int model) {
  if (maxTheta <= 0.0001) return 0.0;
  if (model == 0) { // Equidistant: r = f * theta
    return maxRadius * (theta / maxTheta);
  } else if (model == 1) { // Equisolid: r = 2 * f * sin(theta/2)
    return maxRadius * (sin(theta * 0.5) / sin(maxTheta * 0.5));
  } else if (model == 2) { // Stereographic: r = 2 * f * tan(theta/2)
    return maxRadius * (tan(theta * 0.5) / tan(maxTheta * 0.5));
  } else if (model == 3) { // Orthographic: r = f * sin(theta)
    return maxRadius * (sin(theta) / sin(maxTheta));
  }
  return maxRadius * (theta / maxTheta);
}

void main() {
  if (uMode == 0) {
    // Mode: Original
    vec2 uv = vCoord * 0.5 + 0.5;
    uv.y = 1.0 - uv.y;
    gl_FragColor = texture2D(uTexture, uv);
    return;
  }

  float maxTheta = uLensFov * 0.5;
  vec2 uvSample = vec2(0.0);

  if (uMode == 1) {
    // Mode: Rectilinear Virtual PTZ
    float halfFov = uVirtualFov * 0.5;
    float tanHalf = tan(halfFov);
    float rx = vCoord.x * tanHalf;
    float ry = vCoord.y * (tanHalf / uAspectRatio);
    float rz = 1.0;
    vec3 ray = normalize(vec3(rx, ry, rz));

    // Apply roll, pitch, yaw
    ray = rotateRoll(ray, uViewAngles.z);
    ray = rotatePitch(ray, uViewAngles.y);
    ray = rotateYaw(ray, uViewAngles.x);

    // Adapt to Mount
    vec3 lensRay = ray;
    if (uMountType == 0) {
      // Ceiling mount: camera points down towards floor (+Z down)
      lensRay = vec3(ray.x, ray.z, -ray.y);
    } else if (uMountType == 2) {
      // Floor mount: camera points up towards ceiling (+Z up)
      lensRay = vec3(ray.x, -ray.z, ray.y);
    }

    vec3 normRay = normalize(lensRay);
    if (normRay.z < -0.05) {
      gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
      return;
    }

    float theta = acos(clamp(normRay.z, -1.0, 1.0));
    if (theta > maxTheta) {
      gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
      return;
    }

    float phi = atan(normRay.y, normRay.x) + uRotation;
    float r = computeRadius(theta, maxTheta, uRadius, uProjectionModel);

    uvSample = uCenter + vec2((r / uTextureAspect) * cos(phi), r * sin(phi));

  } else if (uMode == 2) {
    // Mode: Panorama 180
    float phi = vCoord.x * (PI * 0.5) + uViewAngles.x + uRotation;
    float t = vCoord.y * 0.5 + 0.5;
    float theta = mix(0.12 * maxTheta, 0.90 * maxTheta, t);
    float r = computeRadius(theta, maxTheta, uRadius, uProjectionModel);

    uvSample = uCenter + vec2((r / uTextureAspect) * cos(phi), r * sin(phi));

  } else if (uMode == 3) {
    // Mode: 360 Cylindrical with Wrap-around
    float scrollU = fract((vCoord.x * 0.5 + 0.5) + uWrapScroll);
    float phi = scrollU * (2.0 * PI) - PI + uViewAngles.x + uRotation;
    float t = vCoord.y * 0.5 + 0.5;
    float theta = mix(0.12 * maxTheta, 0.90 * maxTheta, t);
    float r = computeRadius(theta, maxTheta, uRadius, uProjectionModel);

    uvSample = uCenter + vec2((r / uTextureAspect) * cos(phi), r * sin(phi));
  }

  // Check bounds
  if (uvSample.x < 0.0 || uvSample.x > 1.0 || uvSample.y < 0.0 || uvSample.y > 1.0) {
    gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
  } else {
    // Flip Y for texture coordinates
    vec2 finalUv = vec2(uvSample.x, 1.0 - uvSample.y);
    gl_FragColor = texture2D(uTexture, finalUv);
  }
}
`;

export interface RenderDewarpOptions {
  mode: FisheyeViewMode;
  calibration: FisheyeCalibration;
  mountType: FisheyeMountType;
  projectionModel: FisheyeProjectionModel;
  view?: Pick<VirtualFisheyeView, "yaw" | "pitch" | "roll" | "fov">;
  wrapScroll?: number;
}

export class FisheyeDewarpEngine {
  private canvas: HTMLCanvasElement;
  private gl: WebGLRenderingContext | null = null;
  private program: WebGLProgram | null = null;
  private texture: WebGLTexture | null = null;
  private quadBuffer: WebGLBuffer | null = null;

  // Uniform locations cache
  private uResolutionLoc: WebGLUniformLocation | null = null;
  private uCenterLoc: WebGLUniformLocation | null = null;
  private uRadiusLoc: WebGLUniformLocation | null = null;
  private uRotationLoc: WebGLUniformLocation | null = null;
  private uLensFovLoc: WebGLUniformLocation | null = null;
  private uProjectionModelLoc: WebGLUniformLocation | null = null;
  private uMountTypeLoc: WebGLUniformLocation | null = null;
  private uModeLoc: WebGLUniformLocation | null = null;
  private uViewAnglesLoc: WebGLUniformLocation | null = null;
  private uVirtualFovLoc: WebGLUniformLocation | null = null;
  private uAspectRatioLoc: WebGLUniformLocation | null = null;
  private uTextureAspectLoc: WebGLUniformLocation | null = null;
  private uWrapScrollLoc: WebGLUniformLocation | null = null;

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    this.initGl();
  }

  public get isSupported(): boolean {
    return !!this.gl && !!this.program;
  }

  private initGl(): void {
    const gl =
      this.canvas.getContext("webgl", {
        alpha: false,
        antialias: true,
        preserveDrawingBuffer: true,
      }) ||
      (this.canvas.getContext("experimental-webgl") as WebGLRenderingContext | null);

    if (!gl) return;
    this.gl = gl;

    const vs = this.compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER);
    const fs = this.compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER);
    if (!vs || !fs) return;

    const prog = gl.createProgram();
    if (!prog) return;
    gl.attachShader(prog, vs);
    gl.attachShader(prog, fs);
    gl.linkProgram(prog);

    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      console.error("Failed to link fisheye program:", gl.getProgramInfoLog(prog));
      return;
    }

    this.program = prog;

    // Cache uniforms
    this.uResolutionLoc = gl.getUniformLocation(prog, "uResolution");
    this.uCenterLoc = gl.getUniformLocation(prog, "uCenter");
    this.uRadiusLoc = gl.getUniformLocation(prog, "uRadius");
    this.uRotationLoc = gl.getUniformLocation(prog, "uRotation");
    this.uLensFovLoc = gl.getUniformLocation(prog, "uLensFov");
    this.uProjectionModelLoc = gl.getUniformLocation(prog, "uProjectionModel");
    this.uMountTypeLoc = gl.getUniformLocation(prog, "uMountType");
    this.uModeLoc = gl.getUniformLocation(prog, "uMode");
    this.uViewAnglesLoc = gl.getUniformLocation(prog, "uViewAngles");
    this.uVirtualFovLoc = gl.getUniformLocation(prog, "uVirtualFov");
    this.uAspectRatioLoc = gl.getUniformLocation(prog, "uAspectRatio");
    this.uTextureAspectLoc = gl.getUniformLocation(prog, "uTextureAspect");
    this.uWrapScrollLoc = gl.getUniformLocation(prog, "uWrapScroll");

    // Quad geometry: full screen 2 triangles
    const quad = new Float32Array([
      -1, -1,
       1, -1,
      -1,  1,
      -1,  1,
       1, -1,
       1,  1,
    ]);
    this.quadBuffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, this.quadBuffer);
    gl.bufferData(gl.ARRAY_BUFFER, quad, gl.STATIC_DRAW);

    // Texture setup
    this.texture = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, this.texture);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
  }

  private compileShader(gl: WebGLRenderingContext, type: number, src: string): WebGLShader | null {
    const s = gl.createShader(type);
    if (!s) return null;
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
      console.error("Fisheye shader compile error:", gl.getShaderInfoLog(s));
      gl.deleteShader(s);
      return null;
    }
    return s;
  }

  public render(
    source: TexImageSource,
    options: RenderDewarpOptions,
  ): boolean {
    const gl = this.gl;
    if (!gl || !this.program || !this.texture || !this.quadBuffer) {
      return false;
    }

    const width = this.canvas.width || 640;
    const height = this.canvas.height || 480;
    gl.viewport(0, 0, width, height);

    gl.useProgram(this.program);

    // Upload video frame to texture
    gl.bindTexture(gl.TEXTURE_2D, this.texture);
    try {
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, source);
    } catch {
      return false;
    }

    // Bind quad buffer
    gl.bindBuffer(gl.ARRAY_BUFFER, this.quadBuffer);
    const posLoc = gl.getAttribLocation(this.program, "aPosition");
    gl.enableVertexAttribArray(posLoc);
    gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, 0, 0);

    // Map mode to integer uniform:
    // 0: original, 1: rectilinear/virtual_ptz, 2: panorama_180, 3: cylindrical_360
    let modeCode = 1;
    if (options.mode === "original") modeCode = 0;
    else if (options.mode === "panorama") modeCode = 2;
    else if (options.mode === "360") modeCode = 3;

    let projCode = 0;
    if (options.projectionModel === "equisolid") projCode = 1;
    else if (options.projectionModel === "stereographic") projCode = 2;
    else if (options.projectionModel === "orthographic") projCode = 3;

    let mountCode = 0;
    if (options.mountType === "wall") mountCode = 1;
    else if (options.mountType === "floor") mountCode = 2;

    const calib = options.calibration;
    const view = options.view || { yaw: 0, pitch: -30, roll: 0, fov: 90 };

    gl.uniform2f(this.uResolutionLoc, width, height);
    gl.uniform2f(this.uCenterLoc, calib.centerX, calib.centerY);
    gl.uniform1f(this.uRadiusLoc, calib.radius);
    gl.uniform1f(this.uRotationLoc, toRad(calib.rotation));
    gl.uniform1f(this.uLensFovLoc, toRad(calib.fov));
    gl.uniform1i(this.uProjectionModelLoc, projCode);
    gl.uniform1i(this.uMountTypeLoc, mountCode);
    gl.uniform1i(this.uModeLoc, modeCode);
    gl.uniform3f(
      this.uViewAnglesLoc,
      toRad(view.yaw),
      toRad(view.pitch),
      toRad(view.roll),
    );
    gl.uniform1f(this.uVirtualFovLoc, toRad(view.fov));
    gl.uniform1f(this.uAspectRatioLoc, width / height);

    let sourceWidth = 1920;
    let sourceHeight = 1080;
    if (source instanceof HTMLVideoElement) {
      sourceWidth = source.videoWidth || 1920;
      sourceHeight = source.videoHeight || 1080;
    } else if (source instanceof HTMLImageElement) {
      sourceWidth = source.naturalWidth || 1920;
      sourceHeight = source.naturalHeight || 1080;
    }
    const textureAspect = sourceHeight > 0 ? sourceWidth / sourceHeight : 1.0;
    gl.uniform1f(this.uTextureAspectLoc, textureAspect);

    gl.uniform1f(this.uWrapScrollLoc, options.wrapScroll || 0);

    gl.drawArrays(gl.TRIANGLES, 0, 6);
    return true;
  }

  public destroy(): void {
    const gl = this.gl;
    if (!gl) return;
    if (this.texture) gl.deleteTexture(this.texture);
    if (this.quadBuffer) gl.deleteBuffer(this.quadBuffer);
    if (this.program) gl.deleteProgram(this.program);
    this.gl = null;
    this.program = null;
  }
}
