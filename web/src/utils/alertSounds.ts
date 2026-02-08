/**
 * Alert sound manager using the Web Audio API.
 *
 * Generates short synthetic tones (no external sound files needed).
 * Respects browser autoplay policies by requiring a user gesture
 * before the AudioContext can start.
 */

let audioCtx: AudioContext | null = null;
let soundEnabled = false;

function getContext(): AudioContext | null {
  if (!audioCtx) {
    try {
      audioCtx = new AudioContext();
    } catch {
      console.warn('Web Audio API not available');
      return null;
    }
  }
  return audioCtx;
}

/**
 * Resume the AudioContext after a user gesture (click / tap).
 * Must be called at least once before sounds will play.
 */
export async function resumeAudio(): Promise<void> {
  const ctx = getContext();
  if (ctx && ctx.state === 'suspended') {
    await ctx.resume();
  }
}

/** Enable or disable alert sounds globally. */
export function setAlertSoundsEnabled(enabled: boolean): void {
  soundEnabled = enabled;
  if (enabled) {
    resumeAudio();
  }
}

/** Whether alert sounds are currently enabled. */
export function isAlertSoundsEnabled(): boolean {
  return soundEnabled;
}

/**
 * Play a short alert tone.
 * @param frequency  Tone frequency in Hz (higher = more urgent)
 * @param duration   Length in seconds
 * @param type       Oscillator wave shape
 */
export function playTone(
  frequency = 880,
  duration = 0.15,
  type: OscillatorType = 'sine',
): void {
  if (!soundEnabled) return;
  const ctx = getContext();
  if (!ctx || ctx.state !== 'running') return;

  const osc = ctx.createOscillator();
  const gain = ctx.createGain();

  osc.type = type;
  osc.frequency.setValueAtTime(frequency, ctx.currentTime);

  gain.gain.setValueAtTime(0.25, ctx.currentTime);
  gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + duration);

  osc.connect(gain);
  gain.connect(ctx.destination);

  osc.start(ctx.currentTime);
  osc.stop(ctx.currentTime + duration);
}

/** Play the critical-alert sound (double beep). */
export function playCriticalAlert(): void {
  playTone(1000, 0.12, 'square');
  setTimeout(() => playTone(1200, 0.12, 'square'), 150);
}

/** Play the edit-war alert sound (lower warble). */
export function playEditWarAlert(): void {
  playTone(660, 0.18, 'triangle');
}

/** Play a generic notification blip. */
export function playNotification(): void {
  playTone(880, 0.1, 'sine');
}
