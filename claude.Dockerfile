FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV IS_SANDBOX=1

# Build args for user setup
ARG USER_ID=1000
ARG GROUP_ID=1000
ARG USERNAME=user

# Install everything
RUN apt-get update && apt-get install -y \
    curl wget git build-essential ca-certificates gnupg tree sudo \
    && curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install Go
RUN wget -q https://go.dev/dl/go1.23.4.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz \
    && rm go1.23.4.linux-amd64.tar.gz

# Install Claude Code (as root, will be available to all users)
RUN curl -fsSL https://claude.ai/install.sh | bash \
    && mv /root/.local/bin/claude /usr/local/bin/ || true

# Create user matching host user
RUN groupadd -g ${GROUP_ID} ${USERNAME} \
    && useradd -u ${USER_ID} -g ${GROUP_ID} -m -s /bin/bash ${USERNAME} \
    && echo "${USERNAME} ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

USER ${USERNAME}

ENV PATH="/usr/local/go/bin:$PATH"

# Clone Avalanche repos to user's home
RUN git clone --depth 1 --branch v1.14.0 https://github.com/ava-labs/avalanchego.git /home/${USERNAME}/avalanchego \
    && git clone --depth 1 --branch v0.8.0 https://github.com/ava-labs/subnet-evm.git /home/${USERNAME}/subnet-evm

WORKDIR /app
CMD ["bash"]
