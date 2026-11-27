import {kernelFile} from './kernel-source.mjs';
export const {watchDownload, downloadKernel, downloadArchive} = await import(kernelFile('downloader/download-watch.mjs'));
