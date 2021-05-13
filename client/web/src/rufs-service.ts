export class RufsShare {
  constructor(
    public local: string,
    public remote: string,
  )
  {}
}

export class RufsCircle {
  constructor(
    public name: string,
    public shares: RufsShare[],
  )
  {}
}

export class RufsConfig {
  constructor(
    public circles: RufsCircle[],
  )
  {}
}

async function rqToError(rq: Response): Promise<string> {
  let error = rq.statusText;
  try {
    const body = await rq.json();
    if (body.error) {
      error = body.error;
    }
  } catch {}
  return error
}

export class RufsService {
  public static async getConfig(): Promise<RufsConfig> {
    const rq = await fetch('/api/config');
    if (!rq.ok) {
      throw new Error('Failed retrieving config: ' + await rqToError(rq));
    }
    const body = await rq.json();
    const circles: RufsCircle[] = [];
    body.Circles.forEach((circle: any) => {
      const shares: RufsShare[] = [];
      circle.Shares.forEach((share: any) => {
        shares.push(new RufsShare(share.Local, share.Remote));
      });
      circles.push(new RufsCircle(circle.Name, shares));
    });
    return new RufsConfig(circles);
  }

  public static async register(circle: string, user: string, token: string, ca: string): Promise<void> {
    const rq = await fetch('/api/register?' + new URLSearchParams({
      circle, user, token, ca
    }));
    if (!rq.ok) {
      throw new Error('Failed to add circle: ' + await rqToError(rq));
    }
  }
}