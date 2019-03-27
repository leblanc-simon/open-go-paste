"use strict";

const webpack = require("webpack");
const path = require("path");
const isProduction = process.argv[process.argv.indexOf('--mode') + 1] === 'production';

let config = {
    mode: isProduction ? 'production' : 'development',
    entry: [
        "@babel/polyfill",
        "webcrypto-shim",
        "whatwg-fetch",
        "./assets/js/app.js"
    ],
    output: {
        path: path.resolve(__dirname, "./assets/js"),
        filename: "./app-bundle.js"
    },
    module: {
        rules: [
            {
                test: /\.js$/,
                exclude: /node_modules/,
                loader: "babel-loader"
            }
        ],
    }
}

module.exports = config;
