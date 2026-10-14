#pragma once
#include "S3Uploader.h"
#include <filesystem>
#include <string>

namespace Fling::Core
{
    struct UploadOutcome
    {
        bool success{ false };
        std::wstring url;
        std::wstring errorMessage;
    };

    class UploadCoordinator
    {
    public:
        // Config check -> S3 upload -> history entry. Blocking and thread-agnostic:
        // no clipboard or toast, so it can run on a worker thread.
        static UploadOutcome Upload(std::filesystem::path const& filePath, ProgressCallback progress = nullptr);

        // Upload() plus clipboard copy and a toast. Clipboard needs the UI thread.
        static UploadOutcome UploadAndShare(std::filesystem::path const& filePath);
    };
}
