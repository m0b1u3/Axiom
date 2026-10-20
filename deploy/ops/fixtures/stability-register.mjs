// Test-only module replacement. Mounted exclusively in an unused acceptance stack.
import {register} from 'node:module';
register(new URL('./stability-loader.mjs', import.meta.url), import.meta.url);
