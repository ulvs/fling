#pragma once
#include <algorithm>
#include <cctype>
#include <filesystem>
#include <string>
#include <unordered_map>

namespace Fling::Core
{
    // The Content-Type a download should get, from the file extension. Browsers use it to
    // decide whether to show or save the file.
    inline std::string ContentTypeFor(std::filesystem::path const& path)
    {
        auto ext = path.extension().string();
        std::transform(ext.begin(), ext.end(), ext.begin(),
            [](unsigned char c) { return static_cast<char>(std::tolower(c)); });

        static const std::unordered_map<std::string, std::string> kMap = {
            { ".txt",  "text/plain" },
            { ".json", "application/json" },
            { ".xml",  "application/xml" },
            { ".html", "text/html" },
            { ".htm",  "text/html" },
            { ".css",  "text/css" },
            { ".js",   "application/javascript" },
            { ".pdf",  "application/pdf" },
            { ".zip",  "application/zip" },
            { ".7z",   "application/x-7z-compressed" },
            { ".tar",  "application/x-tar" },
            { ".gz",   "application/gzip" },
            { ".png",  "image/png" },
            { ".jpg",  "image/jpeg" },
            { ".jpeg", "image/jpeg" },
            { ".gif",  "image/gif" },
            { ".webp", "image/webp" },
            { ".svg",  "image/svg+xml" },
            { ".mp3",  "audio/mpeg" },
            { ".mp4",  "video/mp4" },
            { ".mov",  "video/quicktime" },
            { ".webm", "video/webm" },
            { ".doc",  "application/msword" },
            { ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document" },
            { ".xls",  "application/vnd.ms-excel" },
            { ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" },
            { ".ppt",  "application/vnd.ms-powerpoint" },
            { ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation" },
        };

        auto it = kMap.find(ext);
        return it != kMap.end() ? it->second : "application/octet-stream";
    }
}
