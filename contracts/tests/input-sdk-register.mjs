// Test-only SDK substitution. Production never imports this registration file.
import {register} from 'node:module';
register(new URL('./input-sdk-loader.mjs',import.meta.url),import.meta.url);
