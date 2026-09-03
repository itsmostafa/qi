class Qi < Formula
  desc "Local-first knowledge search CLI"
  homepage "https://github.com/itsmostafa/qi"
  url "https://github.com/itsmostafa/qi/archive/refs/tags/v0.9.1.tar.gz"
  sha256 "362113fd1e4162a5921aefa224a5aa80a1819d58de7706caac963039fe8ac1c0"
  license "MIT"
  head "https://github.com/itsmostafa/qi.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/itsmostafa/qi/internal/version.Version=v#{version}
      -X github.com/itsmostafa/qi/internal/version.BuildDate=#{time.iso8601}
    ]
    ldflags << "-X github.com/itsmostafa/qi/internal/version.Commit=#{Utils.git_short_head}" if build.head?
    system "go", "build", *std_go_args(ldflags: ldflags)
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/qi --version")
  end
end
