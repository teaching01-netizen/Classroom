// qrdecode.swift — decode a QR code from an image file, an http(s) URL, or a
// base64 data URI (like the qr_url returned by the API).
//
// Usage:
//   swift qrdecode.swift <path-or-url-or-data-uri>
//
// Prints the decoded QR payload to stdout. Exit code 0 = decoded, 1 = none.
//
// Zero external dependencies: uses the macOS Vision framework.

import AppKit
import Foundation
import Vision

guard CommandLine.arguments.count >= 2 else {
    FileHandle.standardError.write(Data("usage: swift qrdecode.swift <path|http(s)-url|data:image/...>\n".utf8))
    exit(2)
}

let input = CommandLine.arguments[1]

func fail(_ message: String) -> Never {
    FileHandle.standardError.write(Data("qrdecode: \(message)\n".utf8))
    exit(1)
}

func loadCGImage(from input: String) -> CGImage? {
    var data: Data?
    if input.hasPrefix("data:") {
        // data:image/png;base64,<payload>
        guard let comma = input.firstIndex(of: ",") else { return nil }
        let b64 = String(input[input.index(after: comma)...])
        data = Data(base64Encoded: b64)
    } else if input.hasPrefix("http://") || input.hasPrefix("https://") {
        data = try? Data(contentsOf: URL(string: input)!)
    } else {
        data = try? Data(contentsOf: URL(fileURLWithPath: input))
    }
    guard let imageData = data, let nsImage = NSImage(data: imageData) else { return nil }
    var rect = NSRect(origin: .zero, size: nsImage.size)
    return nsImage.cgImage(forProposedRect: &rect, context: nil, hints: nil)
}

guard let cgImage = loadCGImage(from: input) else {
    fail("could not read image from: \(input)")
}

let request = VNDetectBarcodesRequest()
request.symbologies = [.qr]

let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
do {
    try handler.perform([request])
} catch {
    fail("vision error: \(error)")
}

let results = request.results ?? []
guard !results.isEmpty else {
    FileHandle.standardError.write(Data("qrdecode: no QR code found\n".utf8))
    exit(1)
}

for observation in results {
    if let payload = observation.payloadStringValue {
        print(payload)
    }
}
exit(0)
