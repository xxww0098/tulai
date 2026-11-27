import {kernelFile} from './kernel-source.mjs';
export const {
  PROFILE_FILE, HOST_DISPLAY_FILE, HOST_DISPLAY_EXPRESSION, validateFingerprintIdentity,
  readProfileIdentity, resolveFingerprintIdentity, validateHostDisplay, readHostDisplay,
  writeHostDisplay, emulatedMedia, buildFingerprintArgs,
} = await import(kernelFile('driver/fingerprint-identity.mjs'));
