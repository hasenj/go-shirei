// The one Java class in a shirei Android app. NativeActivity alone cannot do
// text input: its NDK showSoftInput is a broken no-op, and without an
// InputConnection IMEs degrade badly. This subclass adds an invisible
// focusable view whose InputConnection forwards IME traffic to native code
// (the SDL/Gio-lineage pattern), plus a reliable keyboard show/hide.
//
// Also provides *generic* bridges for extensions: startActivityForResult and
// requestPermissions, with results forwarded to native — so feature packages
// (camera, etc.) do not need to fork this class.
//
// Compiled by mobilerun with javac + d8 into classes.dex; the native methods
// live in androidbackend (jni.c). Package/class name is load-bearing: JNI
// symbol names and the manifest activity entry depend on
// dev.shirei.host.ShireiActivity.
package dev.shirei.host;

import android.app.NativeActivity;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ActivityInfo;
import android.content.res.Configuration;
import android.os.Bundle;
import android.view.InputDevice;
import android.view.KeyCharacterMap;
import android.view.KeyEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.inputmethod.BaseInputConnection;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputConnection;
import android.view.inputmethod.InputMethodManager;

public class ShireiActivity extends NativeActivity {
	static {
		// NativeActivity dlopens the lib natively (loadNativeCode), which
		// does NOT register it with the Java classloader — without this
		// loadLibrary, the Java_* natives below throw UnsatisfiedLinkError.
		System.loadLibrary("shireiapp");
	}

	// IME → native (implemented in androidbackend/jni.c; called on UI thread).
	private static native void nativeCommitText(String text);
	private static native void nativeDeleteBackward(int count);
	private static native void nativeSetComposition(String text);
	private static native void nativeFinishComposition();
	private static native void nativeEditorAction(int action);

	// Generic extension bridges → Go (androidbackend).
	private static native void nativeActivityResult(int requestCode, int resultCode, Intent data);
	private static native void nativeRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults);

	private ShireiEditView edit;

	@Override
	protected void onCreate(Bundle savedInstanceState) {
		super.onCreate(savedInstanceState);
		// 1x1 view in the corner: never visible, never in the way of touches,
		// exists purely to own IME focus and the InputConnection.
		edit = new ShireiEditView(this);
		addContentView(edit, new ViewGroup.LayoutParams(1, 1));
	}

	// Called from native (any thread) when the frame's WantsKeyboard changes.
	public void setKeyboardVisible(final boolean show) {
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				InputMethodManager imm =
					(InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
				if (imm == null || edit == null) return;
				if (show) {
					edit.setFocusable(true);
					edit.setFocusableInTouchMode(true);
					edit.requestFocus();
					imm.showSoftInput(edit, 0);
				} else {
					imm.hideSoftInputFromWindow(edit.getWindowToken(), 0);
				}
			}
		});
	}

	// Called from native when Host.PreferredOrientation changes.
	// mode matches shirei.PreferredOrientation: 0 any, 1 portrait, 2 landscape.
	public void setPreferredOrientation(final int mode) {
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				int o;
				switch (mode) {
				case 1:
					o = ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT;
					break;
				case 2:
					o = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE;
					break;
				default:
					o = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED;
					break;
				}
				setRequestedOrientation(o);
			}
		});
	}

	// Physical keyboard present? Built-in / dock / Bluetooth QWERTY counts;
	// soft IME alone does not. Called from the native app thread each frame.
	public boolean hasHardwareKeyboard() {
		Configuration cfg = getResources().getConfiguration();
		if (cfg != null && cfg.keyboard != Configuration.KEYBOARD_NOKEYS) {
			return true;
		}
		int[] ids = InputDevice.getDeviceIds();
		if (ids == null) return false;
		for (int id : ids) {
			InputDevice d = InputDevice.getDevice(id);
			if (d == null || d.isVirtual()) continue;
			if (d.getKeyboardType() == InputDevice.KEYBOARD_TYPE_ALPHABETIC) {
				return true;
			}
		}
		return false;
	}

	// Clipboard, called from the native app thread via JNI. Writes hop to the
	// UI thread fire-and-forget; reads block briefly on a latch (ClipboardManager
	// is picky about which threads may touch it on newer Android).
	public void setClipboardText(final String s) {
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				android.content.ClipboardManager cm = (android.content.ClipboardManager)
					getSystemService(Context.CLIPBOARD_SERVICE);
				if (cm != null) {
					cm.setPrimaryClip(android.content.ClipData.newPlainText("text", s));
				}
			}
		});
	}

	public String getClipboardText() {
		final String[] out = new String[1];
		final java.util.concurrent.CountDownLatch latch =
			new java.util.concurrent.CountDownLatch(1);
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				try {
					android.content.ClipboardManager cm = (android.content.ClipboardManager)
						getSystemService(Context.CLIPBOARD_SERVICE);
					if (cm != null && cm.hasPrimaryClip()) {
						android.content.ClipData clip = cm.getPrimaryClip();
						if (clip != null && clip.getItemCount() > 0) {
							CharSequence cs = clip.getItemAt(0).coerceToText(ShireiActivity.this);
							if (cs != null) out[0] = cs.toString();
						}
					}
				} finally {
					latch.countDown();
				}
			}
		});
		try {
			latch.await(300, java.util.concurrent.TimeUnit.MILLISECONDS);
		} catch (InterruptedException e) {
			Thread.currentThread().interrupt();
		}
		return out[0] == null ? "" : out[0];
	}

	// Open a URL in the system browser (or the scheme's handler). Called from
	// the native app thread via JNI; Intent work runs on the UI thread.
	public void openURL(final String url) {
		if (url == null || url.length() == 0) return;
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				try {
					android.net.Uri uri = android.net.Uri.parse(url);
					android.content.Intent intent = new android.content.Intent(
						android.content.Intent.ACTION_VIEW, uri);
					startActivity(intent);
				} catch (Exception e) {
					// No handler / malformed URL — ignore like iOS openURL.
				}
			}
		});
	}

	// ---- generic extension host (no feature-specific logic) ----------------

	/** Called from native: start an activity for result on the UI thread. */
	public void startActivityForResultFromNative(final Intent intent, final int requestCode) {
		if (intent == null) return;
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				try {
					startActivityForResult(intent, requestCode);
				} catch (Exception e) {
					nativeActivityResult(requestCode, RESULT_CANCELED, null);
				}
			}
		});
	}

	/** Called from native: request runtime permissions on the UI thread. */
	public void requestPermissionsFromNative(final String[] permissions, final int requestCode) {
		if (permissions == null || permissions.length == 0) return;
		runOnUiThread(new Runnable() {
			@Override
			public void run() {
				if (android.os.Build.VERSION.SDK_INT >= 23) {
					requestPermissions(permissions, requestCode);
				} else {
					int[] granted = new int[permissions.length];
					for (int i = 0; i < granted.length; i++) {
						granted[i] = android.content.pm.PackageManager.PERMISSION_GRANTED;
					}
					nativeRequestPermissionsResult(requestCode, permissions, granted);
				}
			}
		});
	}

	/** Called from native: check a single permission (0 = granted). */
	public int checkSelfPermissionFromNative(String permission) {
		if (permission == null) return -1;
		if (android.os.Build.VERSION.SDK_INT < 23) return 0;
		return checkSelfPermission(permission);
	}

	@Override
	protected void onActivityResult(int requestCode, int resultCode, Intent data) {
		super.onActivityResult(requestCode, resultCode, data);
		nativeActivityResult(requestCode, resultCode, data);
	}

	@Override
	public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
		super.onRequestPermissionsResult(requestCode, permissions, grantResults);
		nativeRequestPermissionsResult(requestCode, permissions, grantResults);
	}

	// Keycode+meta → unicode, for the native input queue's hardware / injected
	// key events (KeyCharacterMap is Java-only). Called from the native app
	// thread. Returns 0 for non-printable keys.
	public static int unicodeOf(int deviceId, int keyCode, int metaState) {
		try {
			KeyCharacterMap map = KeyCharacterMap.load(deviceId);
			return map.get(keyCode, metaState);
		} catch (Exception e) {
			return 0;
		}
	}

	class ShireiEditView extends View {
		ShireiEditView(Context ctx) {
			super(ctx);
			setFocusable(true);
			setFocusableInTouchMode(true);
		}

		@Override
		public boolean onCheckIsTextEditor() {
			return true;
		}

		@Override
		public InputConnection onCreateInputConnection(EditorInfo outAttrs) {
			outAttrs.inputType = EditorInfo.TYPE_CLASS_TEXT
				| EditorInfo.TYPE_TEXT_FLAG_MULTI_LINE;
			outAttrs.imeOptions = EditorInfo.IME_FLAG_NO_FULLSCREEN
				| EditorInfo.IME_FLAG_NO_EXTRACT_UI;
			return new ShireiInputConnection(this);
		}
	}

	class ShireiInputConnection extends BaseInputConnection {
		ShireiInputConnection(View target) {
			super(target, true); // full editor: IMEs may compose
		}

		@Override
		public boolean commitText(CharSequence text, int newCursorPosition) {
			nativeCommitText(text.toString());
			return super.commitText(text, newCursorPosition);
		}

		@Override
		public boolean setComposingText(CharSequence text, int newCursorPosition) {
			nativeSetComposition(text.toString());
			return super.setComposingText(text, newCursorPosition);
		}

		@Override
		public boolean finishComposingText() {
			nativeFinishComposition();
			return super.finishComposingText();
		}

		@Override
		public boolean deleteSurroundingText(int beforeLength, int afterLength) {
			if (beforeLength > 0) nativeDeleteBackward(beforeLength);
			return super.deleteSurroundingText(beforeLength, afterLength);
		}

		@Override
		public boolean performEditorAction(int actionCode) {
			nativeEditorAction(actionCode);
			return true;
		}

		@Override
		public boolean sendKeyEvent(KeyEvent event) {
			if (event.getAction() == KeyEvent.ACTION_DOWN) {
				switch (event.getKeyCode()) {
				case KeyEvent.KEYCODE_DEL:
					nativeDeleteBackward(1);
					return true;
				case KeyEvent.KEYCODE_ENTER:
					nativeCommitText("\n");
					return true;
				}
			}
			return super.sendKeyEvent(event);
		}
	}
}
