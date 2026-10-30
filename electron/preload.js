'use strict';
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('psNative', {
  fetchJSON: (url, opts) => ipcRenderer.invoke('net:json', url, opts)
});
