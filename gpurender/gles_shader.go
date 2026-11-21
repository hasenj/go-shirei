//go:build linux || android || js

package gpurender

// GLES 3.00 / WebGL2 shader. Shared by the native EGL submit (Wayland,
// Android) and the wasm WebGL2 submit. WebGL2 is GLES 3.0.
//
// ndc.y = ndcY.x * pos.y / viewport.y + ndcY.y
// destFlip 0: sceneY = gl_FragCoord.y - destOrig.y  (Wayland FBO: GL y=0 is UI top)
// destFlip 1: sceneY = viewport.y - (gl_FragCoord.y - destOrig.y)  (EGL window / WebGL)

const glesVertSrc = `#version 300 es
precision highp float;
precision highp int;
layout(location=0) in vec4 aDst;
layout(location=1) in vec4 aUV;
layout(location=2) in vec4 aColor;
layout(location=3) in vec4 aColor2;
uniform vec2 viewport;
uniform vec2 ndcY;
out vec4 vColor;
out vec4 vColor2;
out vec2 vUV;
out vec2 vCorner;
void main() {
    vec2 c = vec2(float((gl_VertexID == 1 || gl_VertexID == 3 || gl_VertexID == 4) ? 1 : 0),
                  float((gl_VertexID == 2 || gl_VertexID == 4 || gl_VertexID == 5) ? 1 : 0));
    vec2 pos = aDst.xy + c * aDst.zw;
    vec2 ndc;
    ndc.x = pos.x / viewport.x * 2.0 - 1.0;
    ndc.y = ndcY.x * pos.y / viewport.y + ndcY.y;
    gl_Position = vec4(ndc, 0.0, 1.0);
    vColor = aColor;
    vColor2 = aColor2;
    vUV = aUV.xy + c * aUV.zw;
    vCorner = c;
}
`

const glesFragSrc = `#version 300 es
precision highp float;
precision highp int;
in vec4 vColor;
in vec4 vColor2;
in vec2 vUV;
in vec2 vCorner;
uniform uint mode;
uniform uint clipN;
uniform vec4 clipRects[4];
uniform vec4 clipRads[4];
uniform vec2 viewport;
uniform vec2 destOrig;
uniform int destFlip;
uniform sampler2D tex;
uniform sampler2D texUV;
out vec4 fragColor;

float sdRoundBox(vec2 p, vec2 b, vec4 r) {
    r.xy = (p.x > 0.0) ? r.xy : r.zw;
    r.x  = (p.y > 0.0) ? r.x  : r.y;
    vec2 q = abs(p) - b + r.x;
    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r.x;
}

float clipCov(vec2 pos) {
    float cov = 1.0;
    for (uint i = 0u; i < clipN; i++) {
        vec4 rc = clipRects[i];
        vec2 halfv = rc.zw * 0.5;
        vec2 p = pos - (rc.xy + halfv);
        p.y = -p.y;
        vec4 rad = clipRads[i];
        vec4 r = vec4(rad.y, rad.z, rad.x, rad.w);
        cov *= clamp(0.5 - sdRoundBox(p, halfv, r), 0.0, 1.0);
    }
    return cov;
}

void main() {
    vec2 local = gl_FragCoord.xy - destOrig;
    float sceneY = destFlip == 0 ? local.y : (viewport.y - local.y);
    vec2 dev = vec2(local.x, sceneY);
    vec4 outc;
    if (mode == 0u) {
        vec4 c = mix(vColor, vColor2, vCorner.y);
        outc = vec4(c.rgb * c.a, c.a);
    } else if (mode == 1u) {
        float cov = texture(tex, vUV).r;
        vec4 c = mix(vColor, vColor2, vCorner.y);
        float a = c.a * cov;
        outc = vec4(c.rgb * a, a);
    } else if (mode == 3u || mode == 4u) {
        float y = texture(tex, vUV).r;
        vec2 uv = texture(texUV, vUV).rg - vec2(0.5);
        vec3 rgb;
        if (mode == 3u) {
            float y1 = (y - 0.0627451) * 1.1643836;
            rgb = vec3(
                y1 + 1.7927411 * uv.y,
                y1 - 0.2132486 * uv.x - 0.5329093 * uv.y,
                y1 + 2.1124018 * uv.x
            );
        } else {
            rgb = vec3(
                y + 1.5748 * uv.y,
                y - 0.187324 * uv.x - 0.468124 * uv.y,
                y + 1.8556 * uv.x
            );
        }
        rgb = clamp(rgb, 0.0, 1.0);
        outc = vec4(rgb * vColor.a, vColor.a);
    } else {
        vec4 t = texture(tex, vUV);
        outc = t * vColor.a;
    }
    fragColor = outc * clipCov(dev);
}
`
