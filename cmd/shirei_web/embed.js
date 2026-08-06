// Include on a parent page that embeds shirei demos in iframes.
// When a demo calls SetupWindow(w,h), it postMessages {source:"shirei",type:"resize",width,height}
// and this script sizes the matching iframe.
//
//   <script src="embed.js"></script>
//   <iframe src="./small/"></iframe>
//
(function () {
  function resizeFrame(source, width, height) {
    var frames = document.querySelectorAll("iframe");
    for (var i = 0; i < frames.length; i++) {
      var f = frames[i];
      if (f.contentWindow === source) {
        f.style.width = width + "px";
        f.style.height = height + "px";
        f.setAttribute("width", String(width));
        f.setAttribute("height", String(height));
        f.style.border = f.style.border || "0";
        f.style.display = "block";
        return;
      }
    }
  }

  window.addEventListener("message", function (e) {
    var d = e.data;
    if (!d || d.source !== "shirei" || d.type !== "resize") return;
    var w = d.width | 0;
    var h = d.height | 0;
    if (w > 0 && h > 0) resizeFrame(e.source, w, h);
  });
})();
