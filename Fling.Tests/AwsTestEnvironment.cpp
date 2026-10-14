#include "pch.h"

namespace
{
    // Mirrors App's process-wide Aws::InitAPI/ShutdownAPI so S3Uploader can be constructed in tests.
    class AwsTestEnvironment : public ::testing::Environment
    {
    public:
        void SetUp() override { Aws::InitAPI(m_options); }
        void TearDown() override { Aws::ShutdownAPI(m_options); }

    private:
        Aws::SDKOptions m_options;
    };

    [[maybe_unused]] auto* const s_awsEnvironment =
        ::testing::AddGlobalTestEnvironment(new AwsTestEnvironment);
}
