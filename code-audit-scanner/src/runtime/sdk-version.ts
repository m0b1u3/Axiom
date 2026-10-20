import {SDK_ADAPTER_POLICY} from '@axiom/contracts';
/** One generated policy defines producer and historical-reader versions. */
export const ACTIVE_SDK_VERSION = SDK_ADAPTER_POLICY.sdkVersion;
export const READABLE_SDK_VERSIONS = SDK_ADAPTER_POLICY.readableSDKVersions;
export type SupportedSDKVersion = typeof READABLE_SDK_VERSIONS[number];
export function supportedSDKVersion(value: unknown): value is SupportedSDKVersion {
  return READABLE_SDK_VERSIONS.some(version => version === value);
}
