#pragma once
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

    // Orchestrates a single file upload:
    //   config -> S3Uploader -> clipboard + toast + history.
    // Intended to be called from either a CLI activation path or a UI handler.
    // Synchronous: blocks until the upload completes. Callers on a UI thread
    // should move this to a worker if freezing the window matters.
    class UploadCoordinator
    {
    public:
        static UploadOutcome UploadAndShare(std::filesystem::path const& filePath);
    };
}
